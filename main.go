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
	"github.com/hashicorp/go-version"
	"github.com/jszwec/csvutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type RDSInfo struct {
	ClusterIdentifier string
	Engine            string
	EngineVersion     string
}
type MinimumSupportedInfo struct {
	Engine                  string
	MinimumSupportedVersion string
	ValidDate               string
}

var (
	//nolint:gochecknoglobals
	rdsCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws_custom",
		Subsystem: "rds",
		Name:      "cluster_count",
		Help:      "Number of RDS",
	},
		[]string{"cluster_identifier", "engine", "engine_version", "eol_status"},
	)
)

func main() {
	interval, err := getInterval()
	if err != nil {
		log.Fatal(err)
	}

	minimumSupportedInfo, err := readEOLInfoCSV()
	if err != nil {
		log.Fatal(err)
	}

	prometheus.MustRegister(rdsCount)

	http.Handle("/metrics", promhttp.Handler())

	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)

		// register metrics as background
		for range ticker.C {
			err := snapshot(minimumSupportedInfo)
			if err != nil {
				log.Fatal(err)
			}
		}
	}()
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func snapshot(minimumSupportedInfo []MinimumSupportedInfo) error {
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
		eolStatus, err := validateEOLStatus(RDSInfo, minimumSupportedInfo)
		if err != nil {
			return fmt.Errorf("failed to validate EOL Status: %w", err)
		}

		labels := prometheus.Labels{
			"cluster_identifier": RDSInfo.ClusterIdentifier,
			"engine":             RDSInfo.Engine,
			"engine_version":     RDSInfo.EngineVersion,
			"eol_status":         eolStatus,
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

func readEOLInfoCSV() ([]MinimumSupportedInfo, error) {
	var eolInfos []MinimumSupportedInfo

	csv, err := ioutil.ReadFile("/etc/minimum_supported_version.csv")
	if err != nil {
		return []MinimumSupportedInfo{}, fmt.Errorf("failed to read CSV file: %w", err)
	}

	if err := csvutil.Unmarshal(csv, &eolInfos); err != nil {
		return []MinimumSupportedInfo{}, fmt.Errorf("failed to unmarshal: %w", err)
	}

	return eolInfos, nil
}

func validateEOLStatus(rdsInfo RDSInfo, minimumSupportedInfos []MinimumSupportedInfo) (string, error) {
	var eolStatus string
	now := time.Now()

	for _, minimumSupportedInfo := range minimumSupportedInfos {
		if minimumSupportedInfo.Engine == rdsInfo.Engine { //nolint:nestif
			result, err := compareEngineVersion(rdsInfo, minimumSupportedInfo)
			if err != nil {
				return "", fmt.Errorf("failed to compare Engine Version: %w", err)
			}
			if result {
				eolStatus, err = validateEOLDate(minimumSupportedInfo.ValidDate, now)
				if err != nil {
					return "", fmt.Errorf("failed to validate EOL Date: %w", err)
				}
			} else {
				eolStatus = "ok"
			}
		}
	}

	return eolStatus, nil
}

func validateEOLDate(validDate string, now time.Time) (string, error) {
	var layout = "2006-01-02"
	var eolStatus string
	const alertHours = 30 * 24   // 30 Days
	const warningHours = 90 * 24 // 90 Days

	dueDate, err := time.Parse(layout, validDate)
	if err != nil {
		return "", fmt.Errorf("failed to parse valid date: %w", err)
	}

	switch {
	case now.After(dueDate):
		eolStatus = "expired"
	case now.After(dueDate.Add(-1 * alertHours * time.Hour)):
		eolStatus = "alert"
	case now.After(dueDate.Add(-1 * warningHours * time.Hour)):
		eolStatus = "warning"
	default:
		eolStatus = "ok"
	}

	return eolStatus, nil
}

func compareEngineVersion(rdsInfo RDSInfo, minimumSupportedInfo MinimumSupportedInfo) (bool, error) {
	engineVersion, err := version.NewVersion(rdsInfo.EngineVersion)
	if err != nil {
		return false, fmt.Errorf("failed to declare engine version: %w", err)
	}

	minimumSupportedVersion, err := version.NewVersion(minimumSupportedInfo.MinimumSupportedVersion)
	if err != nil {
		return false, fmt.Errorf("failed to declare minimum supported version: %w", err)
	}

	return engineVersion.LessThan(minimumSupportedVersion), nil
}
