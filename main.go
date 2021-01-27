package main

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type RDSInfo struct {
	ClusterIdentifier string
	Engine            string
	EngineVersion     string
}

var (
	//nolint:gochecknoglobals
	rdsCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws_custom",
		Subsystem: "rds",
		Name:      "cluster_count",
		Help:      "Number of RDS",
	},
		[]string{"cluster_identifier", "engine", "engine_version"},
	)
)

func main() {
	interval, err := getInterval()
	if err != nil {
		log.Fatal(err)
	}

	prometheus.MustRegister(rdsCount)

	http.Handle("/metrics", promhttp.Handler())

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)

		// register metrics as background
		for range ticker.C {
			err := snapshot()
			if err != nil {
				log.Fatal(err)
			}
		}
	}()
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func snapshot() error {
	rdsCount.Reset()

	RDSInfos, err := getRDSClusters()
	if err != nil {
		return fmt.Errorf("failed to read RDS infos: %w", err)
	}

	for _, RDSInfo := range RDSInfos {

		labels := prometheus.Labels{
			"cluster_identifier": RDSInfo.ClusterIdentifier,
			"engine":             RDSInfo.Engine,
			"engine_version":     RDSInfo.EngineVersion,
		}
		rdsCount.With(labels).Set(1)
	}

	return nil
}

func getInterval() (int, error) {
	const defaultGithubAPIIntervalSecond = 300
	githubAPIInterval := os.Getenv("AWS_API_INTERVAL")
	if len(githubAPIInterval) == 0 {
		return defaultGithubAPIIntervalSecond, nil
	}

	integerGithubAPIInterval, err := strconv.Atoi(githubAPIInterval)
	if err != nil {
		return 0, fmt.Errorf("failed to read Datadog Config: %w", err)
	}

	return integerGithubAPIInterval, nil
}

func getRDSClusters() ([]RDSInfo, error){
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := rds.New(sess)
	input := &rds.DescribeDBClustersInput{}

	RDSClusters, err := svc.DescribeDBClusters(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case rds.ErrCodeDBClusterNotFoundFault:
				return nil, fmt.Errorf("RDS Cluster is not found: %w", aerr.Error())
			default:
				return nil, fmt.Errorf("failed to describe DB clusters: %w", aerr.Error())
			}
		} else {
			return nil, fmt.Errorf("failed to describe DB clusters: %w", err)
		}
	}

	RDSInfos := make([]RDSInfo, len(RDSClusters.DBClusters))
	for i, RDSCluster := range RDSClusters.DBClusters {

		RDSInfos[i] = RDSInfo{
			ClusterIdentifier: *RDSCluster.DBClusterIdentifier,
			Engine:            *RDSCluster.Engine,
			EngineVersion:     *RDSCluster.EngineVersion,
		}
	}

	return RDSInfos, nil
}