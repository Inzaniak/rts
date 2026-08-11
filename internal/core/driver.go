package core

import "context"

type Driver interface {
	ID() Harness
	DisplayName() string
	Detect(context.Context) []Installation
	Discover(context.Context, string) ([]Resource, error)
	PlanCreate(context.Context, Request) (ChangeSet, error)
	PlanUpdate(context.Context, Resource, []byte) (ChangeSet, error)
	PlanDelete(context.Context, Resource) (ChangeSet, error)
	PlanToggle(context.Context, Resource, bool) (ChangeSet, error)
	Validate(context.Context, Resource) []Diagnostic
	Docs() []string
}
