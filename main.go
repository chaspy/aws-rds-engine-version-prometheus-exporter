package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/jszwec/csvutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type RDSInfo struct {
	ClusterIdentifier string
	Engine            string
	EngineVersion     string
}
type EOLInfo struct {
	Engine           string
	EOLEngineVersion string
	EOLDate          string
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
	eolinfo, err := readEOLInfoCSV()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(eolinfo)

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

	ClusterInfos, err := getRDSClusters()
	if err != nil {
		return fmt.Errorf("failed to read RDS Cluster infos: %w", err)
	}

	InstanceInfos, err := getRDSInstances()
	if err != nil {
		return fmt.Errorf("failed to read RDS Instance infos: %w", err)
	}

	RDSInfos := append(ClusterInfos, InstanceInfos...)

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

func getRDSClusters() ([]RDSInfo, error) {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := rds.New(sess)
	input := &rds.DescribeDBClustersInput{}

	RDSClusters, err := svc.DescribeDBClusters(input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe DB clusters: %w", err)
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

// Get information about RDS Instances that are not Aurora
// nolint:funlen
func getRDSInstances() ([]RDSInfo, error) {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := rds.New(sess)
	input := &rds.DescribeDBInstancesInput{
		// Supported engine versions are referenced here
		// https://docs.aws.amazon.com/cli/latest/reference/rds/describe-db-engine-versions.html#options
		Filters: []*rds.Filter{
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("mariadb")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("mysql")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("oracle-ee")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("oracle-se2")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("oracle-se1")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("oracle-se")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("postgres")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("sqlserver-ee")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("sqlserver-se")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("sqlserver-ex")},
			},
			{
				Name:   aws.String("engine"),
				Values: []*string{aws.String("sqlserver-web")},
			},
		},
	}

	RDSInstances, err := svc.DescribeDBInstances(input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe DB instances: %w", err)
	}

	RDSInfos := make([]RDSInfo, len(RDSInstances.DBInstances))
	for i, RDSInstance := range RDSInstances.DBInstances {
		RDSInfos[i] = RDSInfo{
			ClusterIdentifier: *RDSInstance.DBInstanceIdentifier,
			Engine:            *RDSInstance.Engine,
			EngineVersion:     *RDSInstance.EngineVersion,
		}
	}

	return RDSInfos, nil
}

func readEOLInfoCSV() ([]EOLInfo, error) {
	var eolInfos []EOLInfo

	csv, err := ioutil.ReadFile("eolinfo.csv")
	if err != nil {
		return []EOLInfo{}, fmt.Errorf("failed to read CSV file: %w", err)
	}

	if err := csvutil.Unmarshal(csv, &eolInfos); err != nil {
		return []EOLInfo{}, fmt.Errorf("failed to unmarshal: %w", err)
	}

	return eolInfos, nil
}
