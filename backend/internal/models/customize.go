package models

// CustomizeItem represents a customization configuration item
// This struct uses reflection tags to define customization categories and metadata
// Categories must be grouped together and use `catmeta` tag for category-level metadata
type CustomizeItem struct {
	// Defaults category
	DefaultProjectTemplate   CustomizeVariable `key:"defaultProjectTemplate" meta:"label=Default Project Template;type=select;keywords=template,default,project,scaffold,boilerplate,starter;category=defaults;description=Set the default template for new projects"`
	DefaultContainerSettings CustomizeVariable `key:"defaultContainerSettings" meta:"label=Default Container Settings;type=object;keywords=container,default,settings,docker,configuration,runtime;category=defaults;description=Configure default container runtime settings"`
	DefaultNetworkMode       CustomizeVariable `key:"defaultNetworkMode" meta:"label=Default Network Mode;type=select;keywords=network,default,mode,bridge,host,none,container;category=defaults;description=Set the default network mode for containers"`

	// Templates category
	CustomTemplates    CustomizeVariable `key:"customTemplates" meta:"label=Custom Templates;type=array;keywords=templates,custom,project,compose,docker-compose,yaml,stack;category=templates;description=Add and manage custom project templates" catmeta:"id=templates;title=Templates;icon=layers;url=/customize/templates;description=Manage project templates and compose file configurations"`
	TemplateCategories CustomizeVariable `key:"templateCategories" meta:"label=Template Categories;type=array;keywords=categories,organization,grouping,tags,classification;category=templates;description=Organize templates into categories"`
	TemplateValidation CustomizeVariable `key:"templateValidation" meta:"label=Template Validation;type=boolean;keywords=validation,check,verify,lint,syntax,schema;category=templates;description=Enable validation for template files"`

	// Registries category
	ContainerRegistries CustomizeVariable `key:"containerRegistries" meta:"label=Container Registries;type=array;keywords=registry,docker,images,hub,private,authentication,credentials;category=registries;description=Manage container registry connections" catmeta:"id=registries;title=Registries;icon=package;url=/customize/registries;description=Configure container registries and authentication"`
	RegistryCredentials CustomizeVariable `key:"registryCredentials" meta:"label=Registry Credentials;type=secure;keywords=credentials,auth,username,password,token,login,security;category=registries;description=Configure authentication for container registries"`
	RegistryMirrors     CustomizeVariable `key:"registryMirrors" meta:"label=Registry Mirrors;type=array;keywords=mirrors,proxy,cache,performance,cdn,regional;category=registries;description=Configure registry mirrors and proxies"`

	// Variables category
	GlobalVariables   CustomizeVariable `key:"globalVariables" meta:"label=Global Variables;type=object;keywords=variables,environment,env,global,config,settings,parameters;category=variables;description=Define reusable variables for all projects" catmeta:"id=variables;title=Variables;icon=code;url=/customize/variables;description=Manage global variables and environment configuration"`
	SecretVariables   CustomizeVariable `key:"secretVariables" meta:"label=Secret Variables;type=secure;keywords=secrets,sensitive,secure,encrypted,password,api,key;category=variables;description=Manage sensitive and encrypted variables"`
	VariableTemplates CustomizeVariable `key:"variableTemplates" meta:"label=Variable Templates;type=array;keywords=templates,reusable,preset,configuration,standard,common;category=variables;description=Create reusable variable configurations"`
}

type CustomizeVariable struct {
	Value string
}
