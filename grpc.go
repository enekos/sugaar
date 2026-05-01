package sugaar

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

// GRPC bundles the sugaar gRPC server. Register your services on Server
// from your application's init code; Run starts the gRPC listener on the
// configured GRPCAddr alongside HTTP/HTTPS.
//
//	pb.RegisterAgentsServer(app.GRPC.Server, &agentsImpl{Hub: app.Hub})
//	app.Run(ctx)
type GRPC struct {
	Server *grpc.Server
	addr   string
	tls    *tls.Config
}

// EnableGRPC initialises a *grpc.Server bound to addr (e.g. ":9090") and
// returns it for service registration. Reflection is enabled so grpcurl
// works out of the box. Pass extra grpc.ServerOption values as needed.
//
//	g := app.EnableGRPC(":9090", grpc.MaxRecvMsgSize(8<<20))
//	pb.RegisterAgentsServer(g.Server, ...)
func (a *App) EnableGRPC(addr string, opts ...grpc.ServerOption) *GRPC {
	srv := grpc.NewServer(opts...)
	reflection.Register(srv)
	a.grpc = &GRPC{Server: srv, addr: addr}
	return a.grpc
}

// EnableGRPCTLS is EnableGRPC with TLS credentials applied.
func (a *App) EnableGRPCTLS(addr string, tlsCfg *tls.Config, opts ...grpc.ServerOption) *GRPC {
	allOpts := append([]grpc.ServerOption{grpc.Creds(credentials.NewTLS(tlsCfg))}, opts...)
	srv := grpc.NewServer(allOpts...)
	reflection.Register(srv)
	a.grpc = &GRPC{Server: srv, addr: addr, tls: tlsCfg}
	return a.grpc
}

// runGRPC starts the gRPC server in a goroutine and returns a stop func.
func (a *App) runGRPC(ctx context.Context) (<-chan error, func(), error) {
	if a.grpc == nil {
		return nil, func() {}, nil
	}
	lis, err := net.Listen("tcp", a.grpc.addr)
	if err != nil {
		return nil, func() {}, err
	}
	a.log.Info("sugaar: serving gRPC", "addr", a.grpc.addr, "tls", a.grpc.tls != nil)

	errCh := make(chan error, 1)
	go func() {
		err := a.grpc.Server.Serve(lis)
		if errors.Is(err, grpc.ErrServerStopped) {
			err = nil
		}
		errCh <- err
	}()

	stop := func() {
		done := make(chan struct{})
		go func() { a.grpc.Server.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			a.grpc.Server.Stop()
		}
	}
	return errCh, stop, nil
}

// AutocertTLSConfig returns a *tls.Config driven by Let's Encrypt for the
// given hostnames. Use it with EnableGRPCTLS to share certs across HTTP and
// gRPC listeners. The cache directory matches Options.AutoCertCacheDir.
func (a *App) AutocertTLSConfig(domains ...string) *tls.Config {
	dir := a.opts.AutoCertCacheDir
	if dir == "" {
		dir = "./certs"
	}
	m := &autocert.Manager{
		Cache:      autocert.DirCache(dir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domains...),
	}
	return &tls.Config{
		GetCertificate: m.GetCertificate,
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"h2", "http/1.1", acme.ALPNProto},
	}
}

// mountHealth registers a minimal /healthz endpoint useful for load
// balancers and Docker HEALTHCHECK. Mounted by default in New(); disable
// via Options.DisableHealth.
func (a *App) mountHealth() {
	a.GET("/healthz", func(c *Context) error { return c.String(http.StatusOK, "ok") })
}
