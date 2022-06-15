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

//nolint:gochecknoglobals
var (
	//nolint:promlinter // It is deprecated
	// deprecated
	rdsCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws_custom",
		Subsystem: "rds",
		Name:      "cluster_count",
		Help:      "Number of RDS",
	},
		[]string{"cluster_identifier", "engine", "engine_version", "eol_status"},
	)

	okCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws_custom",
		Subsystem: "rds",
		Name:      "eol_status_ok",
		Help:      "Number of instances whose EOL status is ok",
	},
		[]string{"cluster_identifier", "engine", "engine_version"},
	)

	warningCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws_custom",
		Subsystem: "rds",
		Name:      "eol_status_warning",
		Help:      "Number of instances whose EOL status is warning",
	},
		[]string{"cluster_identifier", "engine", "engine_version"},
	)

	alertCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws_custom",
		Subsystem: "rds",
		Name:      "eol_status_alert",
		Help:      "Number of instances whose EOL status is alert",
	},
		[]string{"cluster_identifier", "engine", "engine_version"},
	)

	expiredCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "aws_custom",
		Subsystem: "rds",
		Name:      "eol_status_expired",
		Help:      "Number of instances whose EOL status is expired",
	},
		[]string{"cluster_identifier", "engine", "engine_version"},
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
	prometheus.MustRegister(expiredCount)
	prometheus.MustRegister(alertCount)
	prometheus.MustRegister(warningCount)
	prometheus.MustRegister(okCount)

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
	okCount.Reset()
	warningCount.Reset()
	alertCount.Reset()
	expiredCount.Reset()

	ClusterInfos, err := getRDSClusters()
	if err != nil {
		return fmt.Errorf("failed to read RDS Cluster infos: %w", err)
	}

	InstanceInfos, err := getRDSInstances()
	if err != nil {
		return fmt.Errorf("failed to read RDS Instance infos: %w", err)
	}

	RDSInfos := ClusterInfos
	RDSInfos = append(RDSInfos, InstanceInfos...)

	for _, RDSInfo := range RDSInfos {
		err := export(RDSInfo, minimumSupportedInfo)
		if err != nil {
			return fmt.Errorf("failed to export metric: %w. skip rdsInfo %#v", err, RDSInfo)
		}
	}

	return nil
}

func export(rdsInfo RDSInfo, minimumSupportedInfo []MinimumSupportedInfo) error {
	eolStatus, err := validateEOLStatus(rdsInfo, minimumSupportedInfo)
	if err != nil {
		return fmt.Errorf("failed to validate EOL Status: %w. skip rdsInfo %#v", err, rdsInfo)
	}

	// deprecated
	labels := prometheus.Labels{
		"cluster_identifier": rdsInfo.ClusterIdentifier,
		"engine":             rdsInfo.Engine,
		"engine_version":     rdsInfo.EngineVersion,
		"eol_status":         eolStatus,
	}
	rdsCount.With(labels).Set(1)

	newLabels := prometheus.Labels{
		"cluster_identifier": rdsInfo.ClusterIdentifier,
		"engine":             rdsInfo.Engine,
		"engine_version":     rdsInfo.EngineVersion,
	}

	switch eolStatus {
	case "expired":
		expiredCount.With(newLabels).Set(1)
		alertCount.With(newLabels).Set(0)
		warningCount.With(newLabels).Set(0)
		okCount.With(newLabels).Set(0)
	case "alert":
		expiredCount.With(newLabels).Set(0)
		alertCount.With(newLabels).Set(1)
		warningCount.With(newLabels).Set(0)
		okCount.With(newLabels).Set(0)
	case "warning":
		expiredCount.With(newLabels).Set(0)
		alertCount.With(newLabels).Set(0)
		warningCount.With(newLabels).Set(1)
		okCount.With(newLabels).Set(0)
	case "ok":
		expiredCount.With(newLabels).Set(0)
		alertCount.With(newLabels).Set(0)
		warningCount.With(newLabels).Set(0)
		okCount.With(newLabels).Set(1)
	default:
		log.Printf("eolStatus is not set. RDSInfo %#v skip", rdsInfo)
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

func getAlertHours() (int, error) {
	const defaultAlertHours = 2160 // 90 days * 24 hour
	alertHours := os.Getenv("ALERT_HOURS")
	if len(alertHours) == 0 {
		return defaultAlertHours, nil
	}

	integerAlertHours, err := strconv.Atoi(alertHours)
	if err != nil {
		return 0, fmt.Errorf("failed to read Alert Hours: %w", err)
	}

	return integerAlertHours, nil
}

func getWarningHours() (int, error) {
	const defaultWarningHours = 4320 // 180 days * 24 hour
	warningHours := os.Getenv("WARNING_HOURS")
	if len(warningHours) == 0 {
		return defaultWarningHours, nil
	}

	integerWarningHours, err := strconv.Atoi(warningHours)
	if err != nil {
		return 0, fmt.Errorf("failed to read Warning Hours: %w", err)
	}

	return integerWarningHours, nil
}

func getRDSClusters() ([]RDSInfo, error) {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))

	svc := rds.New(sess)
	var nextToken *string
	more := true
	RDSInfos := make([]RDSInfo, 0)

	for more == true {
		RDSClusters, err := svc.DescribeDBClusters(&rds.DescribeDBClustersInput{
			Marker: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to describe DB clusters: %w", err)
		}
		for _, RDSCluster := range RDSClusters.DBClusters {
			RDSInfo := RDSInfo{
				ClusterIdentifier: *RDSCluster.DBClusterIdentifier,
				Engine:            *RDSCluster.Engine,
				EngineVersion:     *RDSCluster.EngineVersion,
			}
			RDSInfos = append(RDSInfos, RDSInfo)
			if RDSClusters.Marker == nil {
				more = false
			} else {
				nextToken = RDSClusters.Marker
			}
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
	var nextToken *string
	more := true
	RDSInfos := make([]RDSInfo, 0)
	for more == true {
		RDSInstances, err := svc.DescribeDBInstances(&rds.DescribeDBInstancesInput{
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
			Marker: nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to describe DB instances: %w", err)
		}
		for _, RDSInstance := range RDSInstances.DBInstances {
			RDSInfo := RDSInfo{
				ClusterIdentifier: *RDSInstance.DBInstanceIdentifier,
				Engine:            *RDSInstance.Engine,
				EngineVersion:     *RDSInstance.EngineVersion,
			}

			RDSInfos = append(RDSInfos, RDSInfo)
			if RDSInstances.Marker == nil {
				more = false
			} else {
				nextToken = RDSInstances.Marker
			}
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

	alertHours, err := getAlertHours()
	if err != nil {
		return "", fmt.Errorf("failed to get Alert Hour: %w", err)
	}

	warningHours, err := getWarningHours()
	if err != nil {
		return "", fmt.Errorf("failed to get Warning Hour: %w", err)
	}

	dueDate, err := time.Parse(layout, validDate)
	if err != nil {
		return "", fmt.Errorf("failed to parse valid date: %w", err)
	}

	switch {
	case now.After(dueDate):
		eolStatus = "expired"
	case now.After(dueDate.Add(-1 * time.Duration(alertHours) * time.Hour)):
		eolStatus = "alert"
	case now.After(dueDate.Add(-1 * time.Duration(warningHours) * time.Hour)):
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
