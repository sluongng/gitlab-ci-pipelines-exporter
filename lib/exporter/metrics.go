package exporter

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/xanzy/go-gitlab"
)

// Registry wraps a pointer of prometheus.Registry
type Registry struct {
	*prometheus.Registry
}

var (
	coverage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_coverage",
			Help: "Coverage of the most recent pipeline",
		},
		[]string{"project", "topics", "ref"},
	)

	lastRunDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_last_run_duration_seconds",
			Help: "Duration of last pipeline run",
		},
		[]string{"project", "topics", "ref"},
	)

	lastRunJobDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_last_job_run_duration_seconds",
			Help: "Duration of last job run",
		},
		[]string{"project", "topics", "ref", "stage", "job_name"},
	)

	lastRunJobStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_last_job_run_status",
			Help: "Status of the most recent job",
		},
		[]string{"project", "topics", "ref", "stage", "job_name", "status"},
	)

	lastRunJobArtifactSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_last_job_run_artifact_size",
			Help: "Filesize of the most recent job artifacts",
		},
		[]string{"project", "topics", "ref", "stage", "job_name"},
	)

	timeSinceLastJobRun = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_time_since_last_job_run_seconds",
			Help: "Elapsed time since most recent GitLab CI job run.",
		},
		[]string{"project", "topics", "ref", "stage", "job_name"},
	)

	jobRunCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_job_run_count",
			Help: "GitLab CI pipeline job run count",
		},
		[]string{"project", "topics", "ref", "stage", "job_name"},
	)

	lastRunID = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_last_run_id",
			Help: "ID of the most recent pipeline",
		},
		[]string{"project", "topics", "ref"},
	)

	lastRunStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_last_run_status",
			Help: "Status of the most recent pipeline",
		},
		[]string{"project", "topics", "ref", "status"},
	)

	runCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_run_count",
			Help: "GitLab CI pipeline run count",
		},
		[]string{"project", "topics", "ref"},
	)

	timeSinceLastRun = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_time_since_last_run_seconds",
			Help: "Elapsed time since most recent GitLab CI pipeline run.",
		},
		[]string{"project", "topics", "ref"},
	)

	pipelineVariables = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gitlab_ci_pipeline_run_count_with_variable",
			Help: "Count of pipelines with variables",
		},
		[]string{"project", "topics", "ref", "pipeline_variables"},
	)

	defaultMetrics = []prometheus.Collector{
		coverage,
		lastRunDuration,
		lastRunJobDuration,
		lastRunJobStatus,
		lastRunJobArtifactSize,
		timeSinceLastJobRun,
		jobRunCount,
		lastRunID,
		lastRunStatus,
		runCount,
		timeSinceLastRun,
		pipelineVariables,
	}
)

// NewRegistry initialize a new registry
func NewRegistry() *Registry {
	return &Registry{prometheus.NewRegistry()}
}

// RegisterDefaultMetrics add all our metrics to the registry
func (r *Registry) RegisterDefaultMetrics() error {
	for _, m := range defaultMetrics {
		if err := r.Register(m); err != nil {
			return fmt.Errorf("could not add provided metric '%v' to the Prometheus registry: %v", m, err)
		}
	}
	return nil
}

// MetricsHandler returns an http handler containing with the desired configuration
func (r *Registry) MetricsHandler(disableOpenMetricsEncoder bool) http.Handler {
	return promhttp.HandlerFor(r, promhttp.HandlerOpts{
		Registry:          r,
		EnableOpenMetrics: !disableOpenMetricsEncoder,
	})
}

func emitStatusMetric(metric *prometheus.GaugeVec, labelValues []string, statuses []string, status string, sparseMetrics bool) {
	// Moved into separate function to reduce cyclomatic complexity
	// List of available statuses from the API spec
	// ref: https://docs.gitlab.com/ee/api/jobs.html#list-pipeline-jobs
	for _, s := range statuses {
		args := append(labelValues, s)
		if s == status {
			metric.WithLabelValues(args...).Set(1)
		} else {
			if sparseMetrics {
				metric.DeleteLabelValues(args...)
			} else {
				metric.WithLabelValues(args...).Set(0)
			}
		}
	}
}

type pipelineVarsFetchOp func(interface{}, int, ...gitlab.RequestOptionFunc) ([]*gitlab.PipelineVariable, *gitlab.Response, error)

func emitPipelineVariablesMetric(c *Client, gauge *prometheus.GaugeVec, details *projectDetails, pipelineID int, fetch pipelineVarsFetchOp, filterRegexp *regexp.Regexp) error {
	// get the pipelines data from API
	c.rateLimit()
	variables, _, err := fetch(details.pID, pipelineID)
	if err != nil {
		return fmt.Errorf("could not fetch pipeline variables for pipeline %d: %s", pipelineID, err.Error())
	}
	if len(variables) > 0 {
		var varValues []string
		for _, v := range variables {
			// only add the variables whose key matches the regex
			if filterRegexp.MatchString(v.Key) {
				varValues = append(varValues, v.Key)
			}
		}
		// only create the metric if there are matching vars
		if len(varValues) > 0 {
			log.Debugf("creating metric for pipelines with variables: %v", varValues)
			gauge.WithLabelValues(augmentLabelValues(details, strings.Join(varValues, ","))...).Inc()
		}
	}
	return nil
}

func variableLabelledCounter(metricName string, labels []string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: metricName}, labels)
}