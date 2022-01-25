# aws-rds-engine-version-prometheus-exporter
Prometheus Exporter for AWS RDS Engine Version

![image.png](image.png)

## How to run

### Local

```
$ go run main.go
```

### Binary

Get the binary file from [Releases](https://github.com/chaspy/aws-rds-engine-version-prometheus-exporter/releases) and run it.

### Docker

```
$ docker run chaspy/aws-rds-engine-version-prometheus-exporter:v0.1.0
```

## Metrics

```
$ curl -s localhost:8080/metrics | grep aws_custom_rds_cluster_count
# HELP aws_custom_rds_cluster_count Number of RDS
# TYPE aws_custom_rds_cluster_count gauge
aws_custom_rds_cluster_count{cluster_identifier="api-postgres-develop-a",engine="aurora-postgresql",engine_version="10.7",eol_status="ok"} 1
aws_custom_rds_cluster_count{cluster_identifier="api-postgres-production-a",engine="aurora-postgresql",engine_version="10.7",eol_status="ok"} 1
aws_custom_rds_cluster_count{cluster_identifier="video-production-a",engine="aurora-postgresql",engine_version="9.6.17",eol_status="ok"} 1
aws_custom_rds_cluster_count{cluster_identifier="video-staging-a",engine="aurora-postgresql",engine_version="9.6.17",eol_status="ok"} 1
```

|metric|description|tags|note|
|---------------------------------|-----------------------------------------------|--------------------------------------------------------------|----------|
|aws_custom_rds_eol_status_ok     |Number of instances whose EOL status is ok     |"cluster_identifier", "engine", "engine_version"              |          |
|aws_custom_rds_eol_status_alert  |Number of instances whose EOL status is alert  |"cluster_identifier", "engine", "engine_version"              |          |
|aws_custom_rds_eol_status_warning|Number of instances whose EOL status is warning|"cluster_identifier", "engine", "engine_version"              |          |
|aws_custom_rds_eol_status_expired|Number of instances whose EOL status is expired|"cluster_identifier", "engine", "engine_version"              |          |
|aws_custom_rds_cluster_count     |Number of RDS                                  |"cluster_identifier", "engine", "engine_version", "eol_status"|DEPRECATED|

## IAM Role

The following policy must be attached to the AWS role to be executed.

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "VisualEditor0",
            "Effect": "Allow",
            "Action": [
                "rds:DescribeDBInstances",
                "rds:DescribeDBClusters"
            ],
            "Resource": "*"
        }
    ]
}
```

## Environment Variable

|name            |required|default|description                                       |
|----------------|--------|-------|--------------------------------------------------|
|ALERT_HOURS     |no      | 2160  | Time to determine "alert" status for EOL dates   |
|WARNING_HOURS   |no      | 4320  | Time to determine "warning" status for EOL dates |
|AWS_API_INTERVAL|no      | 300   | Interval between calls to the AWS API            |

## Datadog Autodiscovery

If you use Datadog, you can use [Kubernetes Integration Autodiscovery](https://docs.datadoghq.com/agent/kubernetes/integrations/?tab=kubernetes) feature.


