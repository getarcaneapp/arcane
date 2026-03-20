package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/database"
)

const (
	BackendSQL  = "sql"
	BackendEtcd = "etcd"

	DefaultNamespace         = "arcane"
	DefaultRequestTimeout    = 5 * time.Second
	DefaultDialTimeout       = 5 * time.Second
	DefaultInlineRecordBytes = 512 * 1024
)

type Config struct {
	Backend        string
	Namespace      string
	RequestTimeout time.Duration

	SQLDB *database.DB

	EtcdEndpoints      []string
	EtcdDialTimeout    time.Duration
	EtcdUsername       string
	EtcdPassword       string
	EtcdTLSCAFile      string
	EtcdTLSCertFile    string
	EtcdTLSKeyFile     string
	MaxInlineValueSize int
}

type Record struct {
	Collection string
	Key        string
	Value      []byte
	Revision   int64
}

type Store interface {
	Backend() string
	Close() error
	GetMeta(ctx context.Context, name string) (string, error)
	PutMeta(ctx context.Context, name, value string) error
	GetRecord(ctx context.Context, collection, key string) (*Record, error)
	PutRecord(ctx context.Context, collection, key string, value []byte) error
	DeleteRecord(ctx context.Context, collection, key string) error
	ListRecords(ctx context.Context, collection string) ([]Record, error)
}

func Open(ctx context.Context, cfg Config) (Store, error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = BackendSQL
	}

	switch backend {
	case BackendSQL:
		return newSQLStore(cfg)
	case BackendEtcd:
		return newEtcdStore(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s", cfg.Backend)
	}
}

type sqlMetaRecord struct {
	Namespace string `gorm:"column:namespace;primaryKey;size:255"`
	Name      string `gorm:"column:name;primaryKey;size:255"`
	Value     string `gorm:"column:value;type:text"`
	UpdatedAt time.Time
}

func (sqlMetaRecord) TableName() string { return "storage_meta" }

type sqlValueRecord struct {
	Namespace  string `gorm:"column:namespace;primaryKey;size:255"`
	Collection string `gorm:"column:collection_name;primaryKey;size:255"`
	RecordKey  string `gorm:"column:record_key;primaryKey;size:255"`
	Value      []byte `gorm:"column:value"`
	Revision   int64  `gorm:"column:revision;not null"`
	UpdatedAt  time.Time
}

func (sqlValueRecord) TableName() string { return "storage_records" }

type sqlStore struct {
	db        *database.DB
	namespace string
}

func newSQLStore(cfg Config) (Store, error) {
	if cfg.SQLDB == nil {
		return nil, fmt.Errorf("sql storage backend requires SQLDB")
	}

	if err := cfg.SQLDB.AutoMigrate(&sqlMetaRecord{}, &sqlValueRecord{}); err != nil {
		return nil, fmt.Errorf("failed to initialize sql storage tables: %w", err)
	}

	return &sqlStore{
		db:        cfg.SQLDB,
		namespace: normalizeNamespace(cfg.Namespace),
	}, nil
}

func (s *sqlStore) Backend() string { return BackendSQL }

func (s *sqlStore) Close() error { return nil }

func (s *sqlStore) GetMeta(ctx context.Context, name string) (string, error) {
	var row sqlMetaRecord
	err := s.db.WithContext(ctx).Where("namespace = ? AND name = ?", s.namespace, name).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read sql storage metadata %s: %w", name, err)
	}
	return row.Value, nil
}

func (s *sqlStore) PutMeta(ctx context.Context, name, value string) error {
	row := sqlMetaRecord{
		Namespace: s.namespace,
		Name:      name,
		Value:     value,
	}
	return s.db.WithContext(ctx).Save(&row).Error
}

func (s *sqlStore) GetRecord(ctx context.Context, collection, key string) (*Record, error) {
	var row sqlValueRecord
	err := s.db.WithContext(ctx).
		Where("namespace = ? AND collection_name = ? AND record_key = ?", s.namespace, collection, key).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read sql storage record %s/%s: %w", collection, key, err)
	}

	return &Record{
		Collection: collection,
		Key:        key,
		Value:      append([]byte(nil), row.Value...),
		Revision:   row.Revision,
	}, nil
}

func (s *sqlStore) PutRecord(ctx context.Context, collection, key string, value []byte) error {
	current, err := s.GetRecord(ctx, collection, key)
	if err != nil {
		return err
	}

	revision := int64(1)
	if current != nil {
		revision = current.Revision + 1
	}

	row := sqlValueRecord{
		Namespace:  s.namespace,
		Collection: collection,
		RecordKey:  key,
		Value:      append([]byte(nil), value...),
		Revision:   revision,
	}
	if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
		return fmt.Errorf("failed to write sql storage record %s/%s: %w", collection, key, err)
	}
	return nil
}

func (s *sqlStore) DeleteRecord(ctx context.Context, collection, key string) error {
	if err := s.db.WithContext(ctx).
		Where("namespace = ? AND collection_name = ? AND record_key = ?", s.namespace, collection, key).
		Delete(&sqlValueRecord{}).Error; err != nil {
		return fmt.Errorf("failed to delete sql storage record %s/%s: %w", collection, key, err)
	}
	return nil
}

func (s *sqlStore) ListRecords(ctx context.Context, collection string) ([]Record, error) {
	var rows []sqlValueRecord
	if err := s.db.WithContext(ctx).
		Where("namespace = ? AND collection_name = ?", s.namespace, collection).
		Order("record_key asc").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to list sql storage records for %s: %w", collection, err)
	}

	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, Record{
			Collection: collection,
			Key:        row.RecordKey,
			Value:      append([]byte(nil), row.Value...),
			Revision:   row.Revision,
		})
	}
	return out, nil
}

type recordEnvelope struct {
	Mode   string `json:"mode"`
	Data   []byte `json:"data,omitempty"`
	Chunks int    `json:"chunks,omitempty"`
}

type etcdStore struct {
	client         *clientv3.Client
	namespace      string
	requestTimeout time.Duration
	maxInlineSize  int
}

func newEtcdStore(ctx context.Context, cfg Config) (Store, error) {
	if len(cfg.EtcdEndpoints) == 0 {
		return nil, fmt.Errorf("etcd storage backend requires endpoints")
	}

	tlsConfig, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	dialTimeout := cfg.EtcdDialTimeout
	if dialTimeout <= 0 {
		dialTimeout = DefaultDialTimeout
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:            cfg.EtcdEndpoints,
		DialTimeout:          dialTimeout,
		Username:             cfg.EtcdUsername,
		Password:             cfg.EtcdPassword,
		TLS:                  tlsConfig,
		MaxCallSendMsgSize:   2 * 1024 * 1024,
		MaxCallRecvMsgSize:   16 * 1024 * 1024,
		RejectOldCluster:     true,
		DialKeepAliveTime:    30 * time.Second,
		DialKeepAliveTimeout: 10 * time.Second,
		Context:              ctx,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	inlineSize := cfg.MaxInlineValueSize
	if inlineSize <= 0 {
		inlineSize = DefaultInlineRecordBytes
	}

	return &etcdStore{
		client:         client,
		namespace:      normalizeNamespace(cfg.Namespace),
		requestTimeout: normalizeRequestTimeout(cfg.RequestTimeout),
		maxInlineSize:  inlineSize,
	}, nil
}

func (s *etcdStore) Backend() string { return BackendEtcd }

func (s *etcdStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *etcdStore) GetMeta(ctx context.Context, name string) (string, error) {
	requestCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	resp, err := s.client.Get(requestCtx, s.metaKey(name))
	if err != nil {
		return "", fmt.Errorf("failed to read etcd storage metadata %s: %w", name, err)
	}
	if len(resp.Kvs) == 0 {
		return "", nil
	}
	return string(resp.Kvs[0].Value), nil
}

func (s *etcdStore) PutMeta(ctx context.Context, name, value string) error {
	requestCtx, cancel := s.withTimeout(ctx)
	defer cancel()
	if _, err := s.client.Put(requestCtx, s.metaKey(name), value); err != nil {
		return fmt.Errorf("failed to write etcd storage metadata %s: %w", name, err)
	}
	return nil
}

func (s *etcdStore) GetRecord(ctx context.Context, collection, key string) (*Record, error) {
	requestCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	resp, err := s.client.Get(requestCtx, s.recordKey(collection, key))
	if err != nil {
		return nil, fmt.Errorf("failed to read etcd storage record %s/%s: %w", collection, key, err)
	}
	if len(resp.Kvs) == 0 {
		return nil, nil
	}

	value, err := s.decodeEnvelope(ctx, collection, key, resp.Kvs[0].Value)
	if err != nil {
		return nil, err
	}

	return &Record{
		Collection: collection,
		Key:        key,
		Value:      value,
		Revision:   resp.Kvs[0].ModRevision,
	}, nil
}

func (s *etcdStore) PutRecord(ctx context.Context, collection, key string, value []byte) error {
	envelope := recordEnvelope{Mode: "inline", Data: value}
	if len(value) > s.maxInlineSize {
		chunkCount := 0
		for start := 0; start < len(value); start += s.maxInlineSize {
			end := start + s.maxInlineSize
			if end > len(value) {
				end = len(value)
			}
			chunkCount++
			if err := s.putBlobChunk(ctx, collection, key, chunkCount, value[start:end]); err != nil {
				return err
			}
		}
		envelope = recordEnvelope{Mode: "chunked", Chunks: chunkCount}
	}

	encoded, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to encode storage record envelope for %s/%s: %w", collection, key, err)
	}

	requestCtx, cancel := s.withTimeout(ctx)
	defer cancel()
	if _, err := s.client.Put(requestCtx, s.recordKey(collection, key), string(encoded)); err != nil {
		return fmt.Errorf("failed to write etcd storage record %s/%s: %w", collection, key, err)
	}
	return nil
}

func (s *etcdStore) DeleteRecord(ctx context.Context, collection, key string) error {
	requestCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	if _, err := s.client.Txn(requestCtx).Then(
		clientv3.OpDelete(s.recordKey(collection, key)),
		clientv3.OpDelete(s.blobPrefix(collection, key), clientv3.WithPrefix()),
	).Commit(); err != nil {
		return fmt.Errorf("failed to delete etcd storage record %s/%s: %w", collection, key, err)
	}
	return nil
}

func (s *etcdStore) ListRecords(ctx context.Context, collection string) ([]Record, error) {
	requestCtx, cancel := s.withTimeout(ctx)
	defer cancel()

	resp, err := s.client.Get(requestCtx, s.recordPrefix(collection), clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	if err != nil {
		return nil, fmt.Errorf("failed to list etcd storage records for %s: %w", collection, err)
	}

	out := make([]Record, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		key := keyFromRecordPath(string(kv.Key))
		value, err := s.decodeEnvelope(ctx, collection, key, kv.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, Record{
			Collection: collection,
			Key:        key,
			Value:      value,
			Revision:   kv.ModRevision,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (s *etcdStore) decodeEnvelope(ctx context.Context, collection, key string, raw []byte) ([]byte, error) {
	var envelope recordEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("failed to decode storage envelope for %s/%s: %w", collection, key, err)
	}

	switch envelope.Mode {
	case "inline":
		return append([]byte(nil), envelope.Data...), nil
	case "chunked":
		if envelope.Chunks <= 0 {
			return nil, fmt.Errorf("invalid chunk count for %s/%s", collection, key)
		}
		var combined []byte
		for i := 1; i <= envelope.Chunks; i++ {
			chunk, err := s.getBlobChunk(ctx, collection, key, i)
			if err != nil {
				return nil, err
			}
			combined = append(combined, chunk...)
		}
		return combined, nil
	default:
		return nil, fmt.Errorf("unknown storage envelope mode for %s/%s: %s", collection, key, envelope.Mode)
	}
}

func (s *etcdStore) putBlobChunk(ctx context.Context, collection, key string, chunkNo int, data []byte) error {
	requestCtx, cancel := s.withTimeout(ctx)
	defer cancel()
	if _, err := s.client.Put(requestCtx, s.blobKey(collection, key, chunkNo), string(data)); err != nil {
		return fmt.Errorf("failed to write blob chunk for %s/%s: %w", collection, key, err)
	}
	return nil
}

func (s *etcdStore) getBlobChunk(ctx context.Context, collection, key string, chunkNo int) ([]byte, error) {
	requestCtx, cancel := s.withTimeout(ctx)
	defer cancel()
	resp, err := s.client.Get(requestCtx, s.blobKey(collection, key, chunkNo))
	if err != nil {
		return nil, fmt.Errorf("failed to read blob chunk for %s/%s: %w", collection, key, err)
	}
	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("missing blob chunk %d for %s/%s", chunkNo, collection, key)
	}
	return append([]byte(nil), resp.Kvs[0].Value...), nil
}

func (s *etcdStore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, normalizeRequestTimeout(s.requestTimeout))
}

func (s *etcdStore) basePrefix() string {
	return "/" + s.namespace + "/v1"
}

func (s *etcdStore) metaKey(name string) string {
	return s.basePrefix() + "/meta/" + name
}

func (s *etcdStore) recordPrefix(collection string) string {
	return s.basePrefix() + "/records/" + collection + "/"
}

func (s *etcdStore) recordKey(collection, key string) string {
	return s.recordPrefix(collection) + key
}

func (s *etcdStore) blobPrefix(collection, key string) string {
	return s.basePrefix() + "/blobs/" + collection + "/" + key + "/"
}

func (s *etcdStore) blobKey(collection, key string, chunkNo int) string {
	return s.blobPrefix(collection, key) + strconv.Itoa(chunkNo)
}

func normalizeNamespace(namespace string) string {
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		return DefaultNamespace
	}
	return namespace
}

func normalizeRequestTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return DefaultRequestTimeout
	}
	return timeout
}

func keyFromRecordPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func buildTLSConfig(cfg Config) (*tls.Config, error) {
	if cfg.EtcdTLSCAFile == "" && cfg.EtcdTLSCertFile == "" && cfg.EtcdTLSKeyFile == "" {
		return nil, nil
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.EtcdTLSCAFile != "" {
		caBytes, err := os.ReadFile(cfg.EtcdTLSCAFile) //nolint:gosec // path comes from explicit config
		if err != nil {
			return nil, fmt.Errorf("failed to read ETCD_TLS_CA_FILE: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("failed to parse ETCD_TLS_CA_FILE")
		}
		tlsConfig.RootCAs = pool
	}

	if cfg.EtcdTLSCertFile != "" || cfg.EtcdTLSKeyFile != "" {
		if cfg.EtcdTLSCertFile == "" || cfg.EtcdTLSKeyFile == "" {
			return nil, fmt.Errorf("ETCD_TLS_CERT_FILE and ETCD_TLS_KEY_FILE must be set together")
		}
		cert, err := tls.LoadX509KeyPair(cfg.EtcdTLSCertFile, cfg.EtcdTLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load etcd TLS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}
