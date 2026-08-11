package core

import "context"

type Driver interface {
	ID() Harness
	DisplayName() string
	Detect(context.Context) []Installation
	Discover(string) ([]Resource, error)
	PlanCreate(Request) (ChangeSet, error)
	PlanUpdate(Resource, []byte) (ChangeSet, error)
	PlanDelete(Resource) (ChangeSet, error)
	PlanEnable(Resource) (ChangeSet, error)
	Validate(Resource) []Diagnostic
	Docs() []string
}
