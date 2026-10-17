package vuepher

import (
	"fmt"
	"sync"
)

// Container is the application's service container (HTTP-12). It is deliberately
// small: it resolves named services and caches singletons. It is not a
// general-purpose dependency injection framework, and it will not guess your
// dependencies by reflecting over constructor signatures.
//
//	bootstrap.Bind("mailer", func(c *vuepher.Container) (any, error) {
//		return mail.NewSMTPMailer(os.Getenv("SMTP_URL"))
//	})
//
// Handlers then resolve it:
//
//	mailer := vuepher.MakeAs[*mail.SMTPMailer](ctx.App().Container, "mailer")
//
// Services registered as singletons are built once, lazily, on first resolution.
type Container struct {
	mu        sync.RWMutex
	bindings  map[string]Binding
	instances map[string]any
	singleton map[string]bool
	building  map[string]bool
}

// Binding builds a service. Returning an error aborts resolution and surfaces
// the cause to the caller.
type Binding func(*Container) (any, error)

// NewContainer creates an empty container.
func NewContainer() *Container {
	return &Container{
		bindings:  map[string]Binding{},
		instances: map[string]any{},
		singleton: map[string]bool{},
		building:  map[string]bool{},
	}
}

// Bind registers a factory that runs on every resolution.
func (c *Container) Bind(name string, factory Binding) *Container {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[name] = factory
	return c
}

// Singleton registers a factory whose result is cached after the first
// resolution.
func (c *Container) Singleton(name string, factory Binding) *Container {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[name] = factory
	c.singleton[name] = true
	return c
}

// Instance registers an already-built value as a singleton.
func (c *Container) Instance(name string, value any) *Container {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.instances[name] = value
	c.singleton[name] = true
	if _, exists := c.bindings[name]; !exists {
		v := value
		c.bindings[name] = func(*Container) (any, error) { return v, nil }
	}
	return c
}

// Make resolves a service by name.
func (c *Container) Make(name string) (any, error) {
	c.mu.RLock()
	if value, ok := c.instances[name]; ok {
		c.mu.RUnlock()
		return value, nil
	}
	factory, ok := c.bindings[name]
	isSingleton := c.singleton[name]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("vuepher: service %q is not registered in the container", name)
	}

	c.mu.Lock()
	if c.building[name] {
		c.mu.Unlock()
		return nil, fmt.Errorf("vuepher: circular dependency detected while building service %q", name)
	}
	c.building[name] = true
	c.mu.Unlock()

	value, err := factory(c)

	c.mu.Lock()
	delete(c.building, name)
	c.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("vuepher: could not build service %q: %w", name, err)
	}
	if isSingleton {
		c.mu.Lock()
		c.instances[name] = value
		c.mu.Unlock()
	}
	return value, nil
}

// MustMake resolves a service and panics when it is unavailable. Intended for
// boot-time wiring, where a missing service is a programming error that should
// stop the application immediately.
func (c *Container) MustMake(name string) any {
	value, err := c.Make(name)
	if err != nil {
		panic(err)
	}
	return value
}

// Bound reports whether a name is registered.
func (c *Container) Bound(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, bound := c.bindings[name]
	return bound
}

// Names returns the registered service names, for `vuepher container:list` and
// debugging.
func (c *Container) Names() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.bindings))
	for name := range c.bindings {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

// MakeAs resolves a service and asserts its type, which keeps handler code free
// of type assertions:
//
//	mailer, err := vuepher.MakeAs[*mail.Mailer](ctx.App().Container, "mailer")
func MakeAs[T any](c *Container, name string) (T, error) {
	var zero T
	value, err := c.Make(name)
	if err != nil {
		return zero, err
	}
	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("vuepher: service %q has type %T, not %T", name, value, zero)
	}
	return typed, nil
}

// MustMakeAs is MakeAs for boot-time wiring; it panics on failure.
func MustMakeAs[T any](c *Container, name string) T {
	value, err := MakeAs[T](c, name)
	if err != nil {
		panic(err)
	}
	return value
}
