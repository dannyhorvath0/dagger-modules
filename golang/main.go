// Build Go projects
//
// A utility module for building, testing, and linting Go projects

package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
)

const (
	DEFAULT_GO = "golang:1.23.4"
	PROJ_MOUNT = "/src"
	LINT_IMAGE = "golangci/golangci-lint:latest"
	OUT_DIR    = "/out/"
)

type Golang struct {
	// +private
	Ctr *Container
	// +private
	Proj *Directory
}

func New(
	// +optional
	ctr *Container,

	// +optional
	proj *Directory,
) *Golang {
	g := &Golang{}

	// Standaard container gebruiken als ctr nil is
	if ctr == nil {
		g.Ctr = dag.Container().From(DEFAULT_GO).
			WithMountedCache("/go/pkg/mod", dag.CacheVolume("gomodcache")).
			WithMountedCache("/root/.cache/go-build", dag.CacheVolume("gobuildcache"))
	} else {
		g.Ctr = ctr
	}

	// Standaard projectdirectory gebruiken als proj nil is
	if proj == nil {
		log.Println("⚠️ Warning: proj is nil. Defaulting to current directory.")
		g.Proj = g.Ctr.Directory(".")
	} else {
		g.Proj = proj
	}

	return g
}

// Build the Go project
func (g *Golang) Build(
	ctx context.Context,
	// The Go source code to build
	// +optional
	source *Directory,
	// Arguments to `go build`
	args []string,
	// The architecture for GOARCH
	// +optional
	arch string,
	// The operating system for GOOS
	// +optional
	os string,
) *Directory {
	if arch == "" {
		arch = runtime.GOARCH
	}
	if os == "" {
		os = runtime.GOOS
	}

	if source != nil {
		g = g.WithProject(source)
	}

	command := append([]string{"go", "build", "-o", OUT_DIR}, args...)
	return g.prepare(ctx).
		WithEnvVariable("GOARCH", arch).
		WithEnvVariable("GOOS", os).
		WithExec(command).
		Directory(OUT_DIR)
}

// Build a Go project returning a Container containing the build
func (g *Golang) BuildContainer(
	ctx context.Context,
	// The Go source code to build
	// +optional
	source *Directory,
	// Arguments to `go build`
	// +optional
	args []string,
	// The architecture for GOARCH
	// +optional
	arch string,
	// The operating system for GOOS
	// +optional
	os string,
	// Base container in which to copy the build
	// +optional
	base *Container,
) *Container {
	dir := g.Build(ctx, source, args, arch, os)
	if base == nil {
		base = dag.Container().From("ubuntu:latest")
	}
	return base.
		WithDirectory("/usr/local/bin/", dir)
}

// Test the Go project
func (g *Golang) Test(
	ctx context.Context,
	// The Go source code to test
	// +optional
	source *Directory,
	// Arguments to `go test`
	// +optional
	// +default "./..."
	component string,
	// Generate a coverprofile or not at a location
	// +optional
	// +default ./
	coverageLocation string,
	// Timeout for go
	// +optional
	// +default "180s"
	timeout string,
) (string, error) {
	if source != nil {
		g = g.WithProject(source)
	}

	command := append([]string{"go", "test", component, "-coverprofile", coverageLocation, "-timeout", timeout, "-v"})

	return g.prepare(ctx).WithExec(command).Stdout(ctx)
}

func (g *Golang) Attach(
	ctx context.Context,
	container *Container,
) (*Container, error) {
	dockerd := g.Service("24.0")

	dockerHost, err := dockerd.Endpoint(ctx, ServiceEndpointOpts{
		Scheme: "tcp",
	})
	if err != nil {
		return nil, err
	}

	return container.
		WithServiceBinding("docker", dockerd).
		WithEnvVariable("DOCKER_HOST", dockerHost), nil
}

// Get a Service container running dockerd
func (g *Golang) Service(
	// +optional
	// +default="24.0"
	dockerVersion string,
) *Service {
	port := 2375
	return dag.Container().
		From(fmt.Sprintf("docker:%s-dind", dockerVersion)).
		WithMountedCache(
			"/var/lib/docker",
			dag.CacheVolume(dockerVersion+"-docker-lib"),
			ContainerWithMountedCacheOpts{
				Sharing: Private,
			}).
		WithExposedPort(port).
		WithExec([]string{
			"dockerd",
			"--host=tcp://0.0.0.0:2375",
			"--host=unix:///var/run/docker.sock",
			"--tls=false",
		}, ContainerWithExecOpts{
			InsecureRootCapabilities: true,
		}).
		AsService()
}

func (g *Golang) Vulncheck(
	ctx context.Context,
	// The Go source code to lint
	// +optional
	source *Directory,
	// Workdir to run golangci-lint
	// +optional
	// +default "./..."
	component string,
) (string, error) {
	if source != nil {
		g = g.WithProject(source)
	}
	g.Ctr = g.prepare(ctx).WithExec([]string{"go", "install", "golang.org/x/vuln/cmd/govulncheck@latest"})
	// return g.prepare().WithExec([]string{"ls", "-latr", component}).Stdout(ctx)
	return g.prepare(ctx).WithExec([]string{"govulncheck", "-C", component}).Stdout(ctx)
}

// Lint the Go project
func (g *Golang) GolangciLint(
	ctx context.Context,
	// The Go source code to lint
	// +optional
	source *Directory,
	// Workdir to run golangci-lint
	// +optional
	// +default "./..."
	component string,
	// Timeout for golangci-lint
	// +optional
	// +default "5m"
	timeout string,
) (string, error) {
	if source != nil {
		g = g.WithProject(source)
	}
	return dag.Container().From(LINT_IMAGE).
		WithMountedDirectory("/src", g.Proj).
		WithWorkdir("/src").
		WithExec([]string{"golangci-lint", "run", "-v", "--allow-parallel-runners", component, "--timeout", timeout}).
		Stdout(ctx)
}

// Sets up the Container with a golang image and cache volumes
func (g *Golang) Base() *Golang {
	g.Ctr = dag.Container().From(DEFAULT_GO).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("gomodcache")).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("gobuildcache")).
		WithMountedDirectory("/src/vendor", g.Proj.Directory("vendor")).
		WithEnvVariable("GOFLAGS", "-mod=vendor")

	return g
}

// The go build container
func (g *Golang) Container() *Container {
	return g.Ctr
}

// The go project directory
func (g *Golang) Project() *Directory {
	return g.Ctr.Directory(PROJ_MOUNT)
}

// Specify the Project to use in the module
func (g *Golang) WithProject(dir *Directory) *Golang {
	g.Proj = dir
	return g
}

// Bring your own container
func (g *Golang) WithContainer(ctr *Container) *Golang {
	g.Ctr = ctr
	return g
}

// Build a remote git repo
func (g *Golang) BuildRemote(
	ctx context.Context,
	remote, ref, module string,
	// +optional
	arch string,
	// +optional
	platform string,
) *Directory {
	git := dag.Git(fmt.Sprintf("https://%s", remote)).
		Branch(ref).
		Tree()
	g = g.WithProject(git)

	if arch == "" {
		arch = runtime.GOARCH
	}
	if platform == "" {
		platform = runtime.GOOS
	}
	command := append([]string{"go", "build", "-o", "build/"}, module)
	return g.prepare(ctx).
		WithEnvVariable("GOARCH", arch).
		WithEnvVariable("GOOS", platform).
		WithExec(command).
		Directory(fmt.Sprintf("%s/%s/", PROJ_MOUNT, "build"))
}

// Private func to check readiness and prepare the container for build/test/lint
func (g *Golang) prepare(ctx context.Context) *Container {
	c := g.Ctr.
		WithDirectory(PROJ_MOUNT, g.Proj).
		WithWorkdir(PROJ_MOUNT)

	c, err := g.Attach(ctx, c)
	if err != nil {
		log.Printf(err.Error())
	}
	return c
}
