package main

import (
	"context"
	"flag"

	"github.com/pingcap/tipocket/cmd/util"
	"github.com/pingcap/tipocket/pkg/cluster"
	"github.com/pingcap/tipocket/pkg/control"
	test_infra "github.com/pingcap/tipocket/pkg/test-infra"
	"github.com/pingcap/tipocket/pkg/test-infra/fixture"
	"github.com/pingcap/tipocket/pkg/verify"
	rwregister "github.com/pingcap/tipocket/tests/rw_register"
)

var (
	tableCount = flag.Int("table-count", 7, "Table count")
	readLock   = flag.String("read-lock", "FOR UPDATE", "Maybe empty or 'FOR UPDATE'")
	txnMode    = flag.String("txn-mode", "pessimistic", "Must be 'pessimistic', 'optimistic' or 'mixed'")
)

func main() {
	var clusterName string
	flag.StringVar(&clusterName, "cluster-name", "", "tidb cluster name")
	flag.Parse()

	if len(clusterName) == 0 {
		clusterName = fixture.Context.Namespace
	}

	suit := util.Suit{
		Config: &control.Config{
			Mode:         control.Mode(fixture.Context.Mode),
			ClientCount:  fixture.Context.ClientCount,
			RequestCount: fixture.Context.RequestCount,
			RunRound:     fixture.Context.RunRound,
			RunTime:      fixture.Context.RunTime,
			History:      fixture.Context.HistoryFile,
		},
		Provider:         cluster.NewDefaultClusterProvider(),
		ClientCreator:    rwregister.NewClientCreator(*tableCount, *readLock, *txnMode, fixture.Context.ReplicaRead),
		NemesisGens:      util.ParseNemesisGenerators(fixture.Context.Nemesis),
		ClientRequestGen: util.OnClientLoop,
		VerifySuit: verify.Suit{
			Checker: rwregister.RegisterChecker{},
			Parser:  rwregister.RegisterParser{},
		},
		ClusterDefs: test_infra.NewDefaultCluster(fixture.Context.Namespace, clusterName,
			fixture.Context.TiDBClusterConfig),
	}
	suit.Run(context.Background())
}
