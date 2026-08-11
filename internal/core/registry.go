package core

import (
	"fmt"
	"sort"
)

type Registry struct {
	drivers map[Harness]Driver
}

func NewRegistry(drivers ...Driver) *Registry {
	r := &Registry{drivers: make(map[Harness]Driver, len(drivers))}
	for _, driver := range drivers {
		r.drivers[driver.ID()] = driver
	}
	return r
}

func (r *Registry) Driver(harness Harness) (Driver, error) {
	driver, ok := r.drivers[harness]
	if !ok {
		return nil, fmt.Errorf("no adapter registered for %s", harness)
	}
	return driver, nil
}

func (r *Registry) Drivers() []Driver {
	result := make([]Driver, 0, len(r.drivers))
	for _, driver := range r.drivers {
		result = append(result, driver)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID() < result[j].ID() })
	return result
}
