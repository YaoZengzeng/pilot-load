package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/grpclog"
	"istio.io/pkg/log"

	"github.com/howardjohn/pilot-load/pkg/simulation"
	"github.com/howardjohn/pilot-load/pkg/simulation/model"
)

var (
	pilotAddress = "localhost:15010"
	metadata     = ""
	kubeconfig   = os.Getenv("KUBECONFIG")
	// TODO scoping, so we can have config dump split from debug
	verbose = false
	cluster = Cluster{}
)

type Cluster struct {
	Namespaces int
	Services   int
	Instances  int
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&pilotAddress, "pilot-address", "p", pilotAddress, "address to pilot")
	rootCmd.PersistentFlags().StringVarP(&metadata, "metadata", "m", metadata, "metadata to send to pilot")
	rootCmd.PersistentFlags().StringVarP(&kubeconfig, "kubeconfig", "k", kubeconfig, "kubeconfig")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", verbose, "verbose")

	rootCmd.PersistentFlags().IntVar(&cluster.Namespaces, "cluster.namespaces", 2, "number of namespaces")
	rootCmd.PersistentFlags().IntVar(&cluster.Services, "cluster.services", 3, "number of services per namespace")
	rootCmd.PersistentFlags().IntVar(&cluster.Instances, "cluster.instances", 4, "number of instances per service")
}

var rootCmd = &cobra.Command{
	Use:          "pilot-load",
	Short:        "open XDS connections to pilot",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if verbose {
			o := log.DefaultOptions()
			for _, s := range log.Scopes() {
				s.SetOutputLevel(log.DebugLevel)
			}
			o.SetOutputLevel(log.DefaultScopeName, log.DebugLevel)
			if err := log.Configure(o); err != nil {
				panic(err.Error())
			}
		}
		grpclog.SetLoggerV2(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))
		sim := ""
		if len(args) > 0 {
			sim = args[0]
		}
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), "/.kube/config")
		}
		a := model.Args{
			PilotAddress: pilotAddress,
			NodeMetadata: metadata,
			KubeConfig:   kubeconfig,
		}

		// TODO read this from config file
		a.Cluster.Namespaces = map[string]model.NamespaceArgs{}
		for namespace := 0; namespace < cluster.Namespaces; namespace++ {
			svc := []model.ServiceArgs{}
			for i := 0; i < cluster.Services; i++ {
				svc = append(svc, model.ServiceArgs{Instances: cluster.Instances})
			}
			a.Cluster.Namespaces[fmt.Sprintf("ns-%d", namespace)] = model.NamespaceArgs{svc}
		}
		switch sim {
		case "cluster":
			return simulation.Cluster(a)
		case "adsc":
			return simulation.Adsc(a)
		default:
			return fmt.Errorf("unknown simulation %v. Expected: {pods, adsc}", sim)
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
