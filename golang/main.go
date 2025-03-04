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
	DEFAULT_GO = "1.24"
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
	if ctr == nil {
		ctr = g.Base(DEFAULT_GO).Ctr
	}
	g.Ctr = ctr

	if proj != nil {
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
func (g *Golang) Testdebug(
	ctx context.Context,
	source *Directory,
	component string,
	timeout string,
) (string, error) {
	if source != nil {
		g = g.WithProject(source)
	}

	// Zorg dat het pad voor coverage.txt bestaat
	_, err := g.Ctr.WithExec([]string{"mkdir", "-p", "/src"}).Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("Failed to create directory /src: %v", err)
	}

	// Voer de tests uit met een relatief pad
	command := append([]string{"go", "test", component, "-coverprofile=/src/overage.txt", "-timeout", timeout, "-v"})
	output, err := g.prepare(ctx).WithExec(command).Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("go test error: %v\nstdout: %s", err, output)
	}

	// Controleer of coverage.txt is aangemaakt
	if _, err := g.Ctr.WithExec([]string{"ls", "-la", "/src"}).Stdout(ctx); err != nil {
		return "", fmt.Errorf("Coverage file not found or not created at: /src")
	}

	return output, nil
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
func (g *Golang) Base(version string) *Golang {
	mod := dag.CacheVolume("gomodcache")
	build := dag.CacheVolume("gobuildcache")
	image := fmt.Sprintf("golang:%s", version)
	c := dag.Container().
		From(image).
		WithMountedCache("/go/pkg/mod", mod).
		WithMountedCache("/root/.cache/go-build", build)
	g.Ctr = c
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
