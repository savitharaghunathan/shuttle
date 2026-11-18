package ate

import (
	"context"
	"fmt"
	"strings"

	"github.com/savitharaghunathan/shuttle/internal/ate/ateapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Control is the ate-api-server surface shuttle uses.
type Control interface {
	GetActor(ctx context.Context, atespace, name string) (*ateapi.Actor, error)
	CreateActor(ctx context.Context, atespace, name, templateName string) (*ateapi.Actor, error)
	ResumeActor(ctx context.Context, atespace, name string) (*ateapi.Actor, error)
	SuspendActor(ctx context.Context, atespace, name string) (*ateapi.Actor, error)
	Close() error
}

// Client is a gRPC Control implementation.
type Client struct {
	conn   *grpc.ClientConn
	api    ateapi.ControlClient
	token  string
	close_ func() error
}

// DialOptions configures the gRPC connection.
type DialOptions struct {
	Endpoint  string
	Token     string
	Insecure  bool
	TLSConfig credentials.TransportCredentials
}

// Dial connects to ate-api-server.
func Dial(ctx context.Context, opts DialOptions) (*Client, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return nil, fmt.Errorf("ate endpoint is required")
	}
	var dialOpts []grpc.DialOption
	switch {
	case opts.TLSConfig != nil:
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(opts.TLSConfig))
	case opts.Insecure:
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	default:
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}
	conn, err := grpc.DialContext(ctx, opts.Endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial ate api %s: %w", opts.Endpoint, err)
	}
	return &Client{conn: conn, api: ateapi.NewControlClient(conn), token: opts.Token, close_: conn.Close}, nil
}

func (c *Client) withAuth(ctx context.Context) context.Context {
	if c.token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
}

func (c *Client) GetActor(ctx context.Context, atespace, name string) (*ateapi.Actor, error) {
	ctx = c.withAuth(ctx)
	actor, err := c.api.GetActor(ctx, &ateapi.GetActorRequest{
		Actor: &ateapi.ObjectRef{Atespace: atespace, Name: name},
	})
	if err != nil {
		return nil, err
	}
	return actor, nil
}

func (c *Client) CreateActor(ctx context.Context, atespace, name, templateName string) (*ateapi.Actor, error) {
	ctx = c.withAuth(ctx)
	return c.api.CreateActor(ctx, &ateapi.CreateActorRequest{
		Actor: &ateapi.Actor{
			Metadata: &ateapi.ResourceMetadata{Atespace: atespace, Name: name},
			ActorTemplate: &ateapi.ObjectRef{
				Atespace: atespace,
				Name:     templateName,
			},
		},
	})
}

func (c *Client) ResumeActor(ctx context.Context, atespace, name string) (*ateapi.Actor, error) {
	ctx = c.withAuth(ctx)
	resp, err := c.api.ResumeActor(ctx, &ateapi.ResumeActorRequest{
		Actor: &ateapi.ObjectRef{Atespace: atespace, Name: name},
	})
	if err != nil {
		return nil, err
	}
	return resp.GetActor(), nil
}

func (c *Client) SuspendActor(ctx context.Context, atespace, name string) (*ateapi.Actor, error) {
	ctx = c.withAuth(ctx)
	resp, err := c.api.SuspendActor(ctx, &ateapi.SuspendActorRequest{
		Actor: &ateapi.ObjectRef{Atespace: atespace, Name: name},
	})
	if err != nil {
		return nil, err
	}
	return resp.GetActor(), nil
}

func (c *Client) Close() error {
	if c.close_ != nil {
		return c.close_()
	}
	return nil
}

// IsNotFound reports whether err is a gRPC NotFound.
func IsNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}

// EnsureActor gets or creates a sticky actor.
func EnsureActor(ctx context.Context, ctrl Control, atespace, name, template string) (*ateapi.Actor, error) {
	actor, err := ctrl.GetActor(ctx, atespace, name)
	if err == nil {
		return actor, nil
	}
	if !IsNotFound(err) {
		return nil, fmt.Errorf("get actor %s/%s: %w", atespace, name, err)
	}
	actor, err = ctrl.CreateActor(ctx, atespace, name, template)
	if err != nil {
		return nil, fmt.Errorf("create actor %s/%s: %w", atespace, name, err)
	}
	return actor, nil
}

