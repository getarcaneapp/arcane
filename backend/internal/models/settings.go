package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	redactionMask     = "XXXXXXXXXX"
	keyAuthOidcConfig = "authOidcConfig"
)

type SettingVariable struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

// IsTrue returns true if the value is a truthy string
func (s SettingVariable) IsTrue() bool {
	ok, _ := strconv.ParseBool(s.Value)
	return ok
}

// AsInt returns the value as an integer
func (s SettingVariable) AsInt() int {
	val, _ := strconv.Atoi(s.Value)
	return val
}

// AsDurationSeconds returns the value as a time.Duration in seconds
func (s SettingVariable) AsDurationSeconds() time.Duration {
	val, err := strconv.Atoi(s.Value)
	if err != nil {
		return 0
	}
	return time.Duration(val) * time.Second
}

type Settings struct {
	// General category
	ProjectsDirectory         SettingVariable `key:"projectsDirectory,envOverride" meta:"label=Projects Directory;type=text;keywords=projects,directory,path,folder,location,storage,files,compose,docker-compose;category=internal;description=Configure where project files are stored"`
	DiskUsagePath             SettingVariable `key:"diskUsagePath" meta:"label=Disk Usage Path;type=text;keywords=disk,usage,path,storage,folder,files;category=general;description=Path used for disk usage calculations"`
	BaseServerURL             SettingVariable `key:"baseServerUrl" meta:"label=Base Server URL;type=text;keywords=base,url,server,domain,host,endpoint,address,link;category=general;description=Set the base URL for the application"`
	EnableGravatar            SettingVariable `key:"enableGravatar" meta:"label=Enable Gravatar;type=boolean;keywords=gravatar,avatar,profile,picture,image,user,photo;category=general;description=Enable Gravatar profile pictures for users"`
	DefaultShell              SettingVariable `key:"defaultShell" meta:"label=Default Shell;type=text;keywords=shell,default,shellpath,path,login;category=general;description=Default shell to use for commands"`
	EnvironmentHealthInterval SettingVariable `key:"environmentHealthInterval" meta:"label=Environment Health Check Interval;type=number;keywords=environment,health,check,interval,frequency,heartbeat,status,monitoring,uptime,jobs,schedule;category=jobschedule;description=How often to check environment connectivity in minutes (default: 2)" catmeta:"id=jobschedule;title=Job Schedule;icon=jobs;url=/settings/jobs;description=Configure how often Arcane background jobs run"`
	AccentColor               SettingVariable `key:"accentColor,public,local" meta:"label=Accent Color;type=text;keywords=color,accent,theme,css,appearance,ui;category=general;description=Primary accent color for UI"`

	// Docker category
	AutoUpdate                 SettingVariable `key:"autoUpdate" meta:"label=Auto Update;type=boolean;keywords=auto,update,automatic,upgrade,refresh,restart,deploy;category=internal;description=Automatically update containers when new images are available"`
	AutoUpdateInterval         SettingVariable `key:"autoUpdateInterval" meta:"label=Auto Update Interval;type=number;keywords=auto,update,interval,frequency,schedule,automatic,timing;category=internal;description=Interval between automatic updates"`
	PollingEnabled             SettingVariable `key:"pollingEnabled" meta:"label=Enable Polling;type=boolean;keywords=polling,check,monitor,watch,scan,detection,automatic;category=internal;description=Enable automatic checking for image updates"`
	PollingInterval            SettingVariable `key:"pollingInterval" meta:"label=Polling Interval;type=number;keywords=interval,frequency,schedule,time,minutes,period,delay;category=internal;description=How often to check for image updates"`
	EventCleanupInterval       SettingVariable `key:"eventCleanupInterval" meta:"label=Event Cleanup Interval;type=number;keywords=events,cleanup,retention,interval,frequency,schedule,history,logs,jobs;category=jobschedule;description=How often to delete old events in minutes"`
	AnalyticsHeartbeatInterval SettingVariable `key:"analyticsHeartbeatInterval" meta:"label=Analytics Heartbeat Interval;type=number;keywords=analytics,heartbeat,interval,frequency,schedule,telemetry,jobs;category=jobschedule;description=How often to send the anonymous analytics heartbeat in minutes"`
	AutoInjectEnv              SettingVariable `key:"autoInjectEnv" meta:"label=Auto Inject Env Variables;type=boolean;keywords=auto,inject,env,environment,variables,interpolation;category=internal;description=Automatically inject project .env variables into all containers (default: false)"`
	PruneMode                  SettingVariable `key:"dockerPruneMode" meta:"label=Docker Prune Action;type=select;keywords=prune,cleanup,clean,remove,delete,unused,dangling,space,disk;category=internal;description=Configure how unused Docker images are cleaned up"`
	ScheduledPruneEnabled      SettingVariable `key:"scheduledPruneEnabled" meta:"label=Scheduled Prune Enabled;type=boolean;keywords=prune,cleanup,maintenance,schedule,automatic;category=internal;description=Enable scheduled pruning of unused Docker resources"`
	ScheduledPruneInterval     SettingVariable `key:"scheduledPruneInterval" meta:"label=Scheduled Prune Interval;type=number;keywords=prune,cleanup,interval,minutes,schedule;category=internal;description=Minutes between scheduled prune runs"`
	ScheduledPruneContainers   SettingVariable `key:"scheduledPruneContainers" meta:"label=Scheduled Prune Containers;type=boolean;keywords=prune,containers,cleanup,maintenance;category=internal;description=Remove stopped containers during scheduled prune"`
	ScheduledPruneImages       SettingVariable `key:"scheduledPruneImages" meta:"label=Scheduled Prune Images;type=boolean;keywords=prune,images,cleanup,maintenance;category=internal;description=Remove unused images during scheduled prune"`
	ScheduledPruneVolumes      SettingVariable `key:"scheduledPruneVolumes" meta:"label=Scheduled Prune Volumes;type=boolean;keywords=prune,volumes,cleanup,maintenance;category=internal;description=Remove unused volumes during scheduled prune"`
	ScheduledPruneNetworks     SettingVariable `key:"scheduledPruneNetworks" meta:"label=Scheduled Prune Networks;type=boolean;keywords=prune,networks,cleanup,maintenance;category=internal;description=Remove unused networks during scheduled prune"`
	ScheduledPruneBuildCache   SettingVariable `key:"scheduledPruneBuildCache" meta:"label=Scheduled Prune Build Cache;type=boolean;keywords=prune,build cache,cleanup,maintenance;category=internal;description=Remove Docker build cache during scheduled prune"`
	MaxImageUploadSize         SettingVariable `key:"maxImageUploadSize" meta:"label=Max Image Upload Size;type=number;keywords=upload,size,limit,maximum,image,tar,file,megabytes,mb,storage;category=internal;description=Maximum size in MB for image archive uploads (default: 500)"`
	DockerHost                 SettingVariable `key:"dockerHost,public,envOverride" meta:"label=Docker Host;type=text;keywords=docker,host,daemon,socket,unix,remote;category=internal;description=URI for Docker daemon"`

	// Security category
	AuthLocalEnabled                SettingVariable `key:"authLocalEnabled,public" meta:"label=Local Authentication;type=boolean;keywords=local,auth,authentication,username,password,login,credentials;category=security;description=Enable local username/password authentication" catmeta:"id=security;title=Security;icon=shield;url=/settings/security;description=Manage authentication and security settings"`
	AuthSessionTimeout              SettingVariable `key:"authSessionTimeout" meta:"label=Session Timeout;type=number;keywords=session,timeout,expire,duration,lifetime,minutes,logout;category=security;description=How long user sessions remain active"`
	AuthPasswordPolicy              SettingVariable `key:"authPasswordPolicy" meta:"label=Password Policy;type=select;keywords=password,policy,strength,complexity,requirements,security,rules;category=security;description=Set password strength requirements"`
	AuthOidcConfig                  SettingVariable `key:"authOidcConfig,sensitive,deprecated" meta:"label=OIDC Config;type=text;keywords=oidc,config,client,id,issuer,secret,oauth;category=security;description=OIDC provider configuration (deprecated - use individual fields)"`
	OidcEnabled                     SettingVariable `key:"oidcEnabled,public,envOverride" meta:"label=OIDC Authentication;type=boolean;keywords=oidc,openid,connect,sso,oauth,external,provider,federation;category=security;description=Enable OpenID Connect (OIDC) authentication"`
	OidcClientId                    SettingVariable `key:"oidcClientId,public,envOverride" meta:"label=OIDC Client ID;type=text;keywords=oidc,client,id,oauth,openid;category=security;description=OIDC provider client ID"`
	OidcClientSecret                SettingVariable `key:"oidcClientSecret,sensitive,envOverride" meta:"label=OIDC Client Secret;type=password;keywords=oidc,client,secret,oauth,openid;category=security;description=OIDC provider client secret"`
	OidcIssuerUrl                   SettingVariable `key:"oidcIssuerUrl,public,envOverride" meta:"label=OIDC Issuer URL;type=text;keywords=oidc,issuer,url,oauth,openid,provider;category=security;description=OIDC provider issuer URL"`
	OidcAuthorizationEndpoint       SettingVariable `key:"oidcAuthorizationEndpoint,envOverride" meta:"label=OIDC Authorization Endpoint;type=text;keywords=oidc,authorization,endpoint,oauth,openid;category=security;description=Override OIDC authorization endpoint"`
	OidcTokenEndpoint               SettingVariable `key:"oidcTokenEndpoint,envOverride" meta:"label=OIDC Token Endpoint;type=text;keywords=oidc,token,endpoint,oauth,openid;category=security;description=Override OIDC token endpoint"`
	OidcUserinfoEndpoint            SettingVariable `key:"oidcUserinfoEndpoint,envOverride" meta:"label=OIDC Userinfo Endpoint;type=text;keywords=oidc,userinfo,endpoint,oauth,openid;category=security;description=Override OIDC userinfo endpoint"`
	OidcJwksEndpoint                SettingVariable `key:"oidcJwksEndpoint,envOverride" meta:"label=OIDC JWKS Endpoint;type=text;keywords=oidc,jwks,keys,endpoint,oauth,openid;category=security;description=Override OIDC JWKS endpoint"`
	OidcDeviceAuthorizationEndpoint SettingVariable `key:"oidcDeviceAuthorizationEndpoint,envOverride" meta:"label=OIDC Device Authorization Endpoint;type=text;keywords=oidc,device,authorization,endpoint,oauth,openid,cli;category=security;description=Override OIDC device authorization endpoint for CLI authentication"`
	OidcScopes                      SettingVariable `key:"oidcScopes,public,envOverride" meta:"label=OIDC Scopes;type=text;keywords=oidc,scopes,oauth,openid,permissions;category=security;description=OIDC scopes to request"`
	OidcAdminClaim                  SettingVariable `key:"oidcAdminClaim,public,envOverride" meta:"label=OIDC Admin Claim;type=text;keywords=oidc,admin,claim,role,group;category=security;description=Claim name for admin role mapping"`
	OidcAdminValue                  SettingVariable `key:"oidcAdminValue,public,envOverride" meta:"label=OIDC Admin Value;type=text;keywords=oidc,admin,value,role,group;category=security;description=Claim value that grants admin access"`
	OidcSkipTlsVerify               SettingVariable `key:"oidcSkipTlsVerify,public,envOverride" meta:"label=OIDC Skip TLS Verify;type=boolean;keywords=oidc,tls,verify,skip,insecure;category=security;description=Skip TLS verification for OIDC provider"`
	OidcMergeAccounts               SettingVariable `key:"oidcMergeAccounts,public,envOverride" meta:"label=OIDC Account Merging;type=boolean;keywords=oidc,merge,link,accounts,email,match,existing,users,combine;category=security;description=Allow OIDC logins to merge with existing accounts by email"`

	// Appearance category
	MobileNavigationMode       SettingVariable `key:"mobileNavigationMode,public,local" meta:"label=Mobile Navigation Mode;type=select;keywords=mode,style,type,floating,docked,position,layout,design,appearance,bottom;category=appearance;description=Choose between floating or docked navigation on mobile" catmeta:"id=appearance;title=Appearance;icon=appearance;url=/settings/appearance;description=Customize navigation, theme, and interface behavior"`
	MobileNavigationShowLabels SettingVariable `key:"mobileNavigationShowLabels,public,local" meta:"label=Show Navigation Labels;type=boolean;keywords=labels,text,icons,display,show,hide,names,captions,titles,visible,toggle;category=appearance;description=Display text labels alongside navigation icons"`
	SidebarHoverExpansion      SettingVariable `key:"sidebarHoverExpansion,public,local" meta:"label=Sidebar Hover Expansion;type=boolean;keywords=sidebar,hover,expansion,expand,desktop,mouse,over,collapsed,collapsible,icon,labels,text,preview,peek,tooltip,overlay,temporary,quick,access,navigation,menu,items,submenu,nested;category=appearance;description=Expand sidebar on hover in desktop mode"`

	// Notifications category (placeholder for category metadata only - actual settings managed via notification service)
	NotificationsCategoryPlaceholder SettingVariable `key:"notificationsCategory,internal" meta:"label=Notifications;type=internal;keywords=notifications,alerts,email,discord,webhooks,events,messages;category=notifications;description=Configure notification providers and alerts" catmeta:"id=notifications;title=Notifications;icon=bell;url=/settings/notifications;description=Configure email and Discord notifications for container and image updates"`

	AgentToken SettingVariable `key:"agentToken,internal,sensitive"`
	InstanceID SettingVariable `key:"instanceId,internal"`

	// Users category (admin management page - no actual settings)
	UsersCategoryPlaceholder SettingVariable `key:"usersCategory,internal" meta:"label=Users;type=internal;keywords=users,accounts,management,admin,access,permissions,roles;category=users;description=Manage user accounts and permissions" catmeta:"id=users;title=Users;icon=user;url=/settings/users;description=Manage user accounts and access control"`

	// API Keys category (admin management page - no actual settings)
	ApiKeysCategoryPlaceholder SettingVariable `key:"apiKeysCategory,internal" meta:"label=API Keys;type=internal;keywords=api,keys,tokens,authentication,access,programmatic,integration;category=apikeys;description=Manage API keys for programmatic access" catmeta:"id=apikeys;title=API Keys;icon=apikey;url=/settings/api-keys;description=Create and manage API keys for programmatic access to Arcane"`

	// Timeout category
	DockerAPITimeout       SettingVariable `key:"dockerApiTimeout,envOverride" meta:"label=Docker API Timeout;type=number;keywords=docker,api,timeout,seconds,list,operations;category=timeouts;description=Timeout for Docker list operations in seconds (default: 30)" catmeta:"id=timeouts;title=Timeouts;icon=clock;url=/settings/timeouts;description=Configure operation timeouts for slow networks or hardware"`
	DockerImagePullTimeout SettingVariable `key:"dockerImagePullTimeout,envOverride" meta:"label=Docker Image Pull Timeout;type=number;keywords=docker,image,pull,timeout,seconds,download;category=timeouts;description=Timeout for Docker image pulls in seconds (default: 600 = 10 minutes)"`
	GitOperationTimeout    SettingVariable `key:"gitOperationTimeout,envOverride" meta:"label=Git Operation Timeout;type=number;keywords=git,clone,timeout,seconds,repository;category=timeouts;description=Timeout for Git clone/fetch operations in seconds (default: 300 = 5 minutes)"`
	HTTPClientTimeout      SettingVariable `key:"httpClientTimeout,envOverride" meta:"label=HTTP Client Timeout;type=number;keywords=http,client,timeout,seconds,api,request;category=timeouts;description=Default timeout for HTTP requests in seconds (default: 30)"`
	RegistryTimeout        SettingVariable `key:"registryTimeout,envOverride" meta:"label=Registry Timeout;type=number;keywords=registry,timeout,seconds,docker,auth;category=timeouts;description=Timeout for container registry operations in seconds (default: 30)"`
	ProxyRequestTimeout    SettingVariable `key:"proxyRequestTimeout,envOverride" meta:"label=Proxy Request Timeout;type=number;keywords=proxy,request,timeout,seconds,forward;category=timeouts;description=Timeout for proxied requests in seconds (default: 60)"`
}

func (SettingVariable) TableName() string {
	return "settings"
}

func (s *Settings) ToSettingVariableSlice(showAll bool, redactSensitiveValues bool) []SettingVariable {
	cfgValue := reflect.ValueOf(s).Elem()
	cfgType := cfgValue.Type()

	var res []SettingVariable

	for i := 0; i < cfgType.NumField(); i++ {
		field := cfgType.Field(i)

		key, attrs, _ := strings.Cut(field.Tag.Get("key"), ",")
		if key == "" {
			continue
		}

		if !showAll && !strings.Contains(attrs, "public") {
			continue
		}

		value := cfgValue.Field(i).FieldByName("Value").String()
		value = redactSettingValue(key, value, attrs, redactSensitiveValues)

		settingVariable := SettingVariable{
			Key:   key,
			Value: value,
		}
		res = append(res, settingVariable)
	}

	return res
}

func (s *Settings) FieldByKey(key string) (defaultValue string, isPublic bool, isSensitive bool, err error) {
	rv := reflect.ValueOf(s).Elem()
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		tagValue := strings.Split(rt.Field(i).Tag.Get("key"), ",")
		keyFromTag := tagValue[0]
		isPublic = slices.Contains(tagValue, "public")
		isSensitive = slices.Contains(tagValue, "sensitive")

		if keyFromTag != key {
			continue
		}

		valueField := rv.Field(i).FieldByName("Value")
		return valueField.String(), isPublic, isSensitive, nil
	}

	return "", false, false, SettingKeyNotFoundError{field: key}
}

func (s *Settings) IsLocalSetting(key string) bool {
	rt := reflect.TypeOf(s).Elem()

	for i := 0; i < rt.NumField(); i++ {
		tagValue := strings.Split(rt.Field(i).Tag.Get("key"), ",")
		keyFromTag := tagValue[0]

		if keyFromTag == key {
			return slices.Contains(tagValue, "local")
		}
	}

	return false
}

func (s *Settings) UpdateField(key string, value string, noSensitive bool) error {
	rv := reflect.ValueOf(s).Elem()
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		tagValue, attrs, _ := strings.Cut(rt.Field(i).Tag.Get("key"), ",")
		if tagValue != key {
			continue
		}

		// If the field is sensitive and noSensitive is true, we skip that
		if noSensitive && strings.Contains(attrs, "sensitive") {
			return SettingSensitiveForbiddenError{field: key}
		}

		valueField := rv.Field(i).FieldByName("Value")
		if !valueField.CanSet() {
			return fmt.Errorf("field Value in SettingVariable is not settable for config key '%s'", key)
		}

		valueField.SetString(value)
		return nil
	}

	return SettingKeyNotFoundError{field: key}
}

// helper keeps redaction logic in one place; behavior unchanged
func redactSettingValue(key, value, attrs string, redact bool) string {
	if value == "" || !redact || !strings.Contains(attrs, "sensitive") {
		return value
	}

	if key == keyAuthOidcConfig {
		var cfg OidcConfig
		if err := json.Unmarshal([]byte(value), &cfg); err == nil {
			cfg.ClientSecret = ""
			if redacted, err := json.Marshal(cfg); err == nil {
				return string(redacted)
			}
			return redactionMask
		}
		return redactionMask
	}

	return redactionMask
}

type SettingKeyNotFoundError struct {
	field string
}

func (e SettingKeyNotFoundError) Error() string {
	return "cannot find setting key '" + e.field + "'"
}

func (e SettingKeyNotFoundError) Is(target error) bool {
	x := SettingKeyNotFoundError{}
	return errors.As(target, &x)
}

type SettingSensitiveForbiddenError struct {
	field string
}

func (e SettingSensitiveForbiddenError) Error() string {
	return "field '" + e.field + "' is sensitive and can't be updated"
}

func (e SettingSensitiveForbiddenError) Is(target error) bool {
	x := SettingSensitiveForbiddenError{}
	return errors.As(target, &x)
}

type OidcConfig struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	IssuerURL    string `json:"issuerUrl"`
	Scopes       string `json:"scopes"`

	AuthorizationEndpoint       string `json:"authorizationEndpoint,omitempty"`
	TokenEndpoint               string `json:"tokenEndpoint,omitempty"`
	UserinfoEndpoint            string `json:"userinfoEndpoint,omitempty"`
	JwksURI                     string `json:"jwksUri,omitempty"`
	DeviceAuthorizationEndpoint string `json:"deviceAuthorizationEndpoint,omitempty"`

	// Admin mapping: evaluate this claim to grant admin.
	// Examples:
	// - adminClaim: "admin", adminValue: "true"        (boolean or string "true")
	// - adminClaim: "roles", adminValue: "admin"       (array membership)
	// - adminClaim: "realm_access.roles", adminValue: "admin" (Keycloak)
	AdminClaim string `json:"adminClaim,omitempty"`
	AdminValue string `json:"adminValue,omitempty"`

	SkipTlsVerify bool `json:"skipTlsVerify"`
}
