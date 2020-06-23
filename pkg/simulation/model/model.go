package model

import (
	"context"
	"fmt"
	"time"

	"istio.io/pkg/log"

	"github.com/howardjohn/pilot-load/pkg/kube"
	"github.com/howardjohn/pilot-load/pkg/simulation/util"
)

type Simulation interface {
	// Run starts the simulation. If the simulation is long lived, this should be done asynchronously,
	// watching ctx.Done() for termination.
	Run(ctx Context) error
	// Cleanup tears down the simulation.
	Cleanup(ctx Context) error
}

type ScalableSimulation interface {
	Scale(ctx Context, delta int) error

	ScaleTo(ctx Context, n int) error
}

type RefreshableSimulation interface {
	// Refresh will make a change to the simulation. This may mean removing and recreating a pod, changing config, etc
	Refresh(ctx Context) error
}

type ServiceArgs struct {
	// Number of instances associated with this service
	Instances int
}

// Cluster defines one single cluster. There is likely only one of these, unless we support multicluster
// A cluster consists of various namespaces
type ClusterArgs struct {
	Namespaces []NamespaceArgs
	Scaler     ScalerSpec
}

type ScalerSpec struct {
	ServicesDelay   time.Duration
	InstancesDelay  time.Duration
	InstancesJitter time.Duration
}

// Namespace defines one Kubernetes namespace
type NamespaceArgs struct {
	// A list of services
	Services []ServiceArgs
}

type Args struct {
	PilotAddress string
	NodeMetadata string
	KubeConfig   string
	Cluster      ClusterArgs
}

type Context struct {
	// Overall context. This should not be used to manage cleanup
	context.Context
	Args   Args
	Client *kube.Client
}

type AggregateSimulation struct {
	Simulations []Simulation
}

var _ Simulation = AggregateSimulation{}

func (a AggregateSimulation) Run(ctx Context) error {
	for _, s := range a.Simulations {
		if util.IsDone(ctx) {
			log.Warnf("exiting early; context cancelled")
			return nil
		}
		log.Debugf("running simulation %T", s)
		if err := s.Run(ctx); err != nil {
			return fmt.Errorf("failed running simulation %T: %v", s, err)
		}
	}
	return nil
}

// TODO parallelize?
func (a AggregateSimulation) Cleanup(ctx Context) error {
	var err error
	for _, s := range a.Simulations {
		log.Debugf("cleaning simulation %T", s)
		if err := s.Cleanup(ctx); err != nil {
			err = util.AddError(err, fmt.Errorf("failed cleaning simulation %T: %v", s, err))
		}
	}
	return err
}
