package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	pkgstorage "github.com/getarcaneapp/arcane/backend/pkg/storage"
)

const (
	storageVersionValue    = "1"
	maxInlineRecordSize    = 512 * 1024
	callbackBeforeUpdateID = "arcane:etcd_before_update"
	callbackAfterUpdateID  = "arcane:etcd_after_update"
	callbackAfterCreateID  = "arcane:etcd_after_create"
	callbackBeforeDeleteID = "arcane:etcd_before_delete"
	callbackAfterDeleteID  = "arcane:etcd_after_delete"
)

type RuntimeStorage struct {
	DB      *database.DB
	Backend string
	Repos   *Repositories

	closeOnce sync.Once
	closeFn   func() error
}

func (s *RuntimeStorage) Close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}

	var err error
	s.closeOnce.Do(func() {
		err = s.closeFn()
	})
	return err
}

type EtcdMirrorStore struct {
	store          pkgstorage.Store
	namespace      string
	requestTimeout time.Duration
	tables         map[string]modelRegistration
	orderedTables  []modelRegistration
}

func InitializeRuntimeStorage(ctx context.Context, cfg *config.Config) (*RuntimeStorage, error) {
	if cfg == nil || cfg.StorageBackend == "" || cfg.StorageBackend == "sql" {
		db, err := database.Initialize(ctx, cfg.DatabaseURL, database.MigrationOptions{
			AllowDowngrade: cfg.AllowDowngrade,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize database: %w", err)
		}

		return &RuntimeStorage{
			DB:      db,
			Backend: "sql",
			Repos:   newSQLRepositories(db),
			closeFn: db.Close,
		}, nil
	}

	if cfg.StorageBackend != "etcd" {
		return nil, fmt.Errorf("unsupported storage backend: %s", cfg.StorageBackend)
	}

	store, err := NewEtcdMirrorStore(ctx, cfg)
	if err != nil {
		return nil, err
	}

	mirrorDB, err := database.Initialize(ctx, "file::memory:?cache=shared", database.MigrationOptions{})
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("failed to initialize etcd mirror database: %w", err)
	}

	if err := store.EnsureMetadata(ctx); err != nil {
		_ = mirrorDB.Close()
		_ = store.Close()
		return nil, err
	}

	if cfg.EtcdImportFromDatabase {
		if err := store.ImportFromSQL(ctx, cfg.DatabaseURL, cfg.AllowDowngrade); err != nil {
			_ = mirrorDB.Close()
			_ = store.Close()
			return nil, err
		}
	}

	if err := store.HydrateMirror(ctx, mirrorDB); err != nil {
		_ = mirrorDB.Close()
		_ = store.Close()
		return nil, err
	}

	if err := store.RegisterMirrorCallbacks(mirrorDB); err != nil {
		_ = mirrorDB.Close()
		_ = store.Close()
		return nil, err
	}

	return &RuntimeStorage{
		DB:      mirrorDB,
		Backend: "etcd",
		Repos:   newEtcdRepositories(store.store),
		closeFn: func() error {
			closeErrs := make([]error, 0, 2)
			if err := mirrorDB.Close(); err != nil {
				closeErrs = append(closeErrs, err)
			}
			if err := store.Close(); err != nil {
				closeErrs = append(closeErrs, err)
			}
			return errors.Join(closeErrs...)
		},
	}, nil
}

func NewEtcdMirrorStore(ctx context.Context, cfg *config.Config) (*EtcdMirrorStore, error) {
	endpoints := cfg.GetEtcdEndpoints()
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("ETCD_ENDPOINTS must be set when STORAGE_BACKEND=etcd")
	}

	store, err := pkgstorage.Open(ctx, pkgstorage.Config{
		Backend:            pkgstorage.BackendEtcd,
		Namespace:          cfg.EtcdNamespace,
		RequestTimeout:     cfg.GetEtcdRequestTimeout(),
		EtcdEndpoints:      endpoints,
		EtcdDialTimeout:    cfg.GetEtcdDialTimeout(),
		EtcdUsername:       cfg.EtcdUsername,
		EtcdPassword:       cfg.EtcdPassword,
		EtcdTLSCAFile:      cfg.EtcdTLSCAFile,
		EtcdTLSCertFile:    cfg.EtcdTLSCertFile,
		EtcdTLSKeyFile:     cfg.EtcdTLSKeyFile,
		MaxInlineValueSize: maxInlineRecordSize,
	})
	if err != nil {
		return nil, err
	}

	tables := make(map[string]modelRegistration, len(registeredModels))
	for _, entry := range registeredModels {
		tables[entry.tableName] = entry
	}

	return &EtcdMirrorStore{
		store:          store,
		namespace:      strings.Trim(cfg.EtcdNamespace, "/"),
		requestTimeout: cfg.GetEtcdRequestTimeout(),
		tables:         tables,
		orderedTables:  slices.Clone(registeredModels),
	}, nil
}

func (s *EtcdMirrorStore) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *EtcdMirrorStore) EnsureMetadata(ctx context.Context) error {
	if s == nil {
		return nil
	}

	if _, err := s.getMeta(ctx, "storage-version"); err != nil {
		return err
	}
	currentVersion, err := s.getMeta(ctx, "storage-version")
	if err != nil {
		return err
	}
	if strings.TrimSpace(currentVersion) == "" {
		if err := s.putMeta(ctx, "storage-version", storageVersionValue); err != nil {
			return fmt.Errorf("failed to initialize storage metadata: %w", err)
		}
	}

	return nil
}

func (s *EtcdMirrorStore) ImportFromSQL(ctx context.Context, databaseURL string, allowDowngrade bool) error {
	importState, err := s.getMeta(ctx, "import-state")
	if err != nil {
		return err
	}
	if strings.TrimSpace(importState) != "" {
		return fmt.Errorf("etcd namespace %q is already initialized; refusing to re-import existing data", s.namespace)
	}

	sourceDB, err := database.Initialize(ctx, databaseURL, database.MigrationOptions{
		AllowDowngrade: allowDowngrade,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize source SQL database for etcd import: %w", err)
	}
	defer func() { _ = sourceDB.Close() }()

	sourceHash := sha256.Sum256([]byte(databaseURL))

	slog.InfoContext(ctx, "Importing SQL data into etcd namespace", "namespace", s.namespace)
	for _, entry := range s.orderedTables {
		rows := entry.newSlice()
		if err := sourceDB.WithContext(ctx).Model(entry.newModel()).Find(rows).Error; err != nil {
			return fmt.Errorf("failed to export table %s from SQL source: %w", entry.tableName, err)
		}

		value := reflect.ValueOf(rows)
		if value.Kind() != reflect.Pointer || value.Elem().Kind() != reflect.Slice {
			return fmt.Errorf("invalid slice registration for table %s", entry.tableName)
		}

		sliceValue := value.Elem()
		for i := 0; i < sliceValue.Len(); i++ {
			if err := s.putRecord(ctx, entry, sliceValue.Index(i).Addr().Interface()); err != nil {
				return fmt.Errorf("failed to import %s row %d: %w", entry.tableName, i, err)
			}
		}
	}

	if err := s.putMeta(ctx, "import-source-hash", hex.EncodeToString(sourceHash[:])); err != nil {
		return err
	}
	if err := s.putMeta(ctx, "import-state", "completed"); err != nil {
		return err
	}

	slog.InfoContext(ctx, "Imported SQL data into etcd namespace", "namespace", s.namespace)
	return nil
}

func (s *EtcdMirrorStore) HydrateMirror(ctx context.Context, db *database.DB) error {
	if s == nil || db == nil {
		return nil
	}

	if err := db.WithContext(ctx).Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		return fmt.Errorf("failed to disable foreign keys during etcd hydration: %w", err)
	}
	defer func(deferredCtx context.Context) {
		_ = db.WithContext(deferredCtx).Exec("PRAGMA foreign_keys = ON").Error
	}(ctx)

	for _, entry := range s.orderedTables {
		rows, err := s.listRecords(ctx, entry)
		if err != nil {
			return err
		}

		if err := db.WithContext(ctx).Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(entry.newModel()).Error; err != nil {
			return fmt.Errorf("failed to clear mirror table %s before hydration: %w", entry.tableName, err)
		}

		rowsValue := reflect.ValueOf(rows)
		if rowsValue.Kind() != reflect.Pointer || rowsValue.Elem().Len() == 0 {
			continue
		}

		if err := db.WithContext(ctx).CreateInBatches(rows, 100).Error; err != nil {
			return fmt.Errorf("failed to hydrate mirror table %s from etcd: %w", entry.tableName, err)
		}
	}

	return nil
}

func (s *EtcdMirrorStore) RegisterMirrorCallbacks(db *database.DB) error {
	if s == nil || db == nil {
		return nil
	}

	if err := db.Callback().Create().After("gorm:create").Register(callbackAfterCreateID, s.afterCreateCallback); err != nil {
		return fmt.Errorf("failed to register etcd create callback: %w", err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(callbackBeforeUpdateID, s.beforeUpdateCallback); err != nil {
		return fmt.Errorf("failed to register etcd before-update callback: %w", err)
	}
	if err := db.Callback().Update().After("gorm:update").Register(callbackAfterUpdateID, s.afterUpdateCallback); err != nil {
		return fmt.Errorf("failed to register etcd after-update callback: %w", err)
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register(callbackBeforeDeleteID, s.beforeDeleteCallback); err != nil {
		return fmt.Errorf("failed to register etcd before-delete callback: %w", err)
	}
	if err := db.Callback().Delete().After("gorm:delete").Register(callbackAfterDeleteID, s.afterDeleteCallback); err != nil {
		return fmt.Errorf("failed to register etcd after-delete callback: %w", err)
	}

	return nil
}

func (s *EtcdMirrorStore) afterCreateCallback(tx *gorm.DB) {
	s.syncRowsAfterMutation(tx, "")
}

func (s *EtcdMirrorStore) beforeUpdateCallback(tx *gorm.DB) {
	s.captureAffectedPrimaryKeys(tx, callbackBeforeUpdateID)
}

func (s *EtcdMirrorStore) afterUpdateCallback(tx *gorm.DB) {
	s.syncRowsAfterMutation(tx, callbackBeforeUpdateID)
}

func (s *EtcdMirrorStore) beforeDeleteCallback(tx *gorm.DB) {
	s.captureAffectedPrimaryKeys(tx, callbackBeforeDeleteID)
}

func (s *EtcdMirrorStore) afterDeleteCallback(tx *gorm.DB) {
	if tx == nil || tx.Error != nil {
		return
	}

	entry, ok := s.lookupEntry(tx.Statement.Table)
	if !ok {
		return
	}

	value, ok := tx.InstanceGet(callbackBeforeDeleteID)
	if !ok {
		return
	}

	keys, _ := value.([]string)
	for _, key := range keys {
		if key == "" {
			continue
		}
		if err := s.deleteRecord(tx.Statement.Context, entry, key); err != nil {
			_ = tx.AddError(err)
			return
		}
	}
}

func (s *EtcdMirrorStore) syncRowsAfterMutation(tx *gorm.DB, instanceKey string) {
	if tx == nil || tx.Error != nil {
		return
	}

	entry, ok := s.lookupEntry(tx.Statement.Table)
	if !ok {
		return
	}

	var rows []any
	switch {
	case instanceKey != "":
		value, ok := tx.InstanceGet(instanceKey)
		if !ok {
			return
		}
		keys, _ := value.([]string)
		if len(keys) == 0 {
			return
		}

		var err error
		rows, err = s.loadRowsByPrimaryKeys(tx.Statement.Context, tx, entry, keys)
		if err != nil {
			_ = tx.AddError(err)
			return
		}
	default:
		var collected bool
		rows, collected = collectRowsFromStatement(tx)
		if !collected {
			var err error
			rows, err = s.queryMatchingRows(tx.Statement.Context, tx, entry)
			if err != nil {
				_ = tx.AddError(err)
				return
			}
		}
	}

	for _, row := range rows {
		if err := s.putRecord(tx.Statement.Context, entry, row); err != nil {
			_ = tx.AddError(err)
			return
		}
	}
}

func (s *EtcdMirrorStore) captureAffectedPrimaryKeys(tx *gorm.DB, instanceKey string) {
	if tx == nil || tx.Error != nil {
		return
	}

	entry, ok := s.lookupEntry(tx.Statement.Table)
	if !ok {
		return
	}

	rows, err := s.queryMatchingRows(tx.Statement.Context, tx, entry)
	if err != nil {
		_ = tx.AddError(err)
		return
	}

	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		key, err := primaryKeyString(row, entry.pkColumn)
		if err != nil {
			_ = tx.AddError(err)
			return
		}
		keys = append(keys, key)
	}
	tx.InstanceSet(instanceKey, keys)
}

func (s *EtcdMirrorStore) queryMatchingRows(ctx context.Context, tx *gorm.DB, entry modelRegistration) ([]any, error) {
	query := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).WithContext(ctx).Model(entry.newModel())
	for _, clauseName := range []string{"WHERE", "LIMIT", "ORDER BY"} {
		if clauseValue, ok := tx.Statement.Clauses[clauseName]; ok {
			query.Statement.Clauses[clauseName] = clauseValue
		}
	}

	dest := entry.newSlice()
	if err := query.Find(dest).Error; err != nil {
		return nil, fmt.Errorf("failed to query changed rows for table %s: %w", entry.tableName, err)
	}

	return sliceToAny(dest), nil
}

func (s *EtcdMirrorStore) loadRowsByPrimaryKeys(ctx context.Context, tx *gorm.DB, entry modelRegistration, keys []string) ([]any, error) {
	rows := make([]any, 0, len(keys))
	for _, key := range keys {
		model := entry.newModel()
		err := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).
			WithContext(ctx).
			Model(entry.newModel()).
			Where(entry.pkColumn+" = ?", key).
			First(model).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("failed to reload %s row %s after mutation: %w", entry.tableName, key, err)
		}
		rows = append(rows, model)
	}
	return rows, nil
}

func (s *EtcdMirrorStore) putRecord(ctx context.Context, entry modelRegistration, row any) error {
	rowBytes, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("failed to encode %s record: %w", entry.tableName, err)
	}

	key, err := primaryKeyString(row, entry.pkColumn)
	if err != nil {
		return err
	}

	if err := s.store.PutRecord(ctx, entry.tableName, key, rowBytes); err != nil {
		return fmt.Errorf("failed to persist %s row %s to storage: %w", entry.tableName, key, err)
	}
	return nil
}

func (s *EtcdMirrorStore) deleteRecord(ctx context.Context, entry modelRegistration, primaryKey string) error {
	if err := s.store.DeleteRecord(ctx, entry.tableName, primaryKey); err != nil {
		return fmt.Errorf("failed to delete %s row %s from storage: %w", entry.tableName, primaryKey, err)
	}
	return nil
}

func (s *EtcdMirrorStore) listRecords(ctx context.Context, entry modelRegistration) (any, error) {
	records, err := s.store.ListRecords(ctx, entry.tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to list %s records from storage: %w", entry.tableName, err)
	}

	rows := entry.newSlice()
	rowsValue := reflect.ValueOf(rows)
	if rowsValue.Kind() != reflect.Pointer || rowsValue.Elem().Kind() != reflect.Slice {
		return nil, fmt.Errorf("invalid slice registration for table %s", entry.tableName)
	}
	sliceValue := rowsValue.Elem()
	for _, record := range records {
		model := entry.newModel()
		if err := json.Unmarshal(record.Value, model); err != nil {
			return nil, fmt.Errorf("failed to decode %s record from storage: %w", entry.tableName, err)
		}
		modelValue := reflect.ValueOf(model)
		if modelValue.Kind() == reflect.Pointer {
			modelValue = modelValue.Elem()
		}
		sliceValue.Set(reflect.Append(sliceValue, modelValue))
	}
	return rows, nil
}

func (s *EtcdMirrorStore) getMeta(ctx context.Context, name string) (string, error) {
	return s.store.GetMeta(ctx, name)
}

func (s *EtcdMirrorStore) putMeta(ctx context.Context, name, value string) error {
	return s.store.PutMeta(ctx, name, value)
}

func (s *EtcdMirrorStore) lookupEntry(tableName string) (modelRegistration, bool) {
	entry, ok := s.tables[tableName]
	return entry, ok
}

func collectRowsFromStatement(tx *gorm.DB) ([]any, bool) {
	if tx == nil || tx.Statement == nil {
		return nil, false
	}

	value := tx.Statement.ReflectValue
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, false
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return nil, false
	}

	kind := value.Kind()
	if kind == reflect.Struct {
		return []any{value.Addr().Interface()}, true
	}
	if kind != reflect.Slice && kind != reflect.Array {
		return nil, false
	}

	rows := make([]any, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		elem := value.Index(i)
		for elem.Kind() == reflect.Pointer {
			if elem.IsNil() {
				break
			}
			elem = elem.Elem()
		}
		if !elem.IsValid() || elem.Kind() != reflect.Struct {
			return nil, false
		}
		rows = append(rows, elem.Addr().Interface())
	}
	return rows, true
}

func sliceToAny(rows any) []any {
	value := reflect.ValueOf(rows)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Slice {
		return nil
	}

	out := make([]any, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		elem := value.Index(i)
		if elem.Kind() == reflect.Struct {
			out = append(out, elem.Addr().Interface())
			continue
		}
		out = append(out, elem.Interface())
	}
	return out
}

func primaryKeyString(row any, primaryKeyColumn string) (string, error) {
	value := reflect.ValueOf(row)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", fmt.Errorf("nil row value")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return "", fmt.Errorf("row %T is not a struct", row)
	}

	typeInfo := value.Type()
	for i := 0; i < typeInfo.NumField(); i++ {
		fieldInfo := typeInfo.Field(i)
		if fieldInfo.Anonymous {
			fieldValue := value.Field(i)
			if fieldValue.Kind() == reflect.Struct || (fieldValue.Kind() == reflect.Pointer && !fieldValue.IsNil()) {
				key, err := primaryKeyString(fieldValue.Interface(), primaryKeyColumn)
				if err == nil && key != "" {
					return key, nil
				}
			}
			continue
		}

		columnName := fieldInfo.Tag.Get("gorm")
		if primaryKeyColumn == strings.TrimSpace(parseColumnName(columnName)) || strings.EqualFold(fieldInfo.Name, primaryKeyColumn) {
			return fmt.Sprint(value.Field(i).Interface()), nil
		}
	}

	fieldName := strings.ToUpper(primaryKeyColumn[:1]) + primaryKeyColumn[1:]
	fieldValue := value.FieldByName(fieldName)
	if fieldValue.IsValid() {
		return fmt.Sprint(fieldValue.Interface()), nil
	}

	return "", fmt.Errorf("primary key %s not found on %T", primaryKeyColumn, row)
}

func parseColumnName(gormTag string) string {
	parts := strings.Split(gormTag, ";")
	for _, part := range parts {
		if after, ok := strings.CutPrefix(part, "column:"); ok {
			return after
		}
	}
	return ""
}
