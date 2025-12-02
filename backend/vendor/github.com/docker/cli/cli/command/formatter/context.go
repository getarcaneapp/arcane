package formatter

const (
	// ClientContextTableFormat is the default client context format.
	ClientContextTableFormat = "table {{.Name}}{{if .Current}} *{{end}}\t{{.Description}}\t{{.DockerEndpoint}}\t{{.Error}}"

	dockerEndpointHeader = "DOCKER ENDPOINT"
	quietContextFormat   = "{{.Name}}"

	maxErrLength = 45
)

// NewClientContextFormat returns a Format for rendering using a Context
func NewClientContextFormat(source string, quiet bool) Format {
	if quiet {
		return quietContextFormat
	}
	if source == TableFormatKey {
		return ClientContextTableFormat
	}
	return Format(source)
}

// ClientContext is a context for display
type ClientContext struct {
	Name           string
	Description    string
	DockerEndpoint string
	Current        bool
	Error          string
}

// ClientContextWrite writes formatted contexts using the Context
func ClientContextWrite(ctx Context, contexts []*ClientContext) error {
	render := func(format func(subContext SubContext) error) error {
		for _, context := range contexts {
			if err := format(&clientContextContext{c: context}); err != nil {
				return err
			}
		}
		return nil
	}
	return ctx.Write(newClientContextContext(), render)
}

type clientContextContext struct {
	HeaderContext
	c *ClientContext
}

func newClientContextContext() *clientContextContext {
	ctx := clientContextContext{}
	ctx.Header = SubHeaderContext{
		"Name":           NameHeader,
		"Description":    DescriptionHeader,
		"DockerEndpoint": dockerEndpointHeader,
		"Error":          ErrorHeader,
	}
	return &ctx
}

func (c *clientContextContext) MarshalJSON() ([]byte, error) {
	return MarshalJSON(c)
}

func (c *clientContextContext) Current() bool {
	return c.c.Current
}

func (c *clientContextContext) Name() string {
	return c.c.Name
}

func (c *clientContextContext) Description() string {
	return c.c.Description
}

func (c *clientContextContext) DockerEndpoint() string {
	return c.c.DockerEndpoint
}

// Error returns the truncated error (if any) that occurred when loading the context.
func (c *clientContextContext) Error() string {
	// TODO(thaJeztah) add "--no-trunc" option to context ls and set default to 30 cols to match "docker service ps"
	return Ellipsis(c.c.Error, maxErrLength)
}
