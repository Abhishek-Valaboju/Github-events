package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Structs to decode GitHub webhook payload
type Repository struct {
	FullName string `json:"full_name"`
}

type WorkflowRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	RunNumber  int       `json:"run_number"`
	StartedAt  time.Time `json:"run_started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Step struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion,omitempty"`
	Number      int       `json:"number"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type WorkflowJob struct {
	ID          int64     `json:"id"`
	RunID       int64     `json:"run_id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Steps       []Step    `json:"steps,omitempty"`
}

type GitHubWebhookPayload struct {
	Action      string      `json:"action"`
	WorkflowRun WorkflowRun `json:"workflow_run,omitempty"`
	WorkflowJob WorkflowJob `json:"workflow_job,omitempty"`
	Repository  Repository  `json:"repository,omitempty"`
}

var (
	workflowRunTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_workflow_run_total",
			Help: "Total number of workflow runs per repository and status",
		},
		[]string{"repository", "workflow", "status"},
	)

	workflowDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "github_actions_workflow_duration_seconds",
			Help:    "Duration of completed workflow runs",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"repository", "workflow", "status"},
	)

	jobRunTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_job_run_total",
			Help: "Total number of jobs run within workflows",
		},
		[]string{"repository", "workflow", "job", "status"},
	)

	jobDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "github_actions_job_duration_seconds",
			Help: "Duration of each job in seconds",
			//Buckets: prometheus.DefBuckets,
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"repository", "workflow", "job", "status"},
	)

	stepRunTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_step_run_total",
			Help: "Total number of steps executed",
		},
		[]string{"repository", "workflow", "job", "step", "status"},
	)

	stepDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "github_actions_step_duration_seconds",
			Help: "Time taken by each individual step",
			//Buckets: prometheus.DefBuckets,
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"repository", "workflow", "job", "step", "status"},
	)

	queuedDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "github_actions_queued_duration_seconds",
			Help:    "Time a workflow spent in queue before starting",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"repository", "workflow"},
	)

	runnersBusy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_actions_runners_busy",
			Help: "Indicates whether a runner is currently executing a job (1 = busy, 0 = idle)",
		},
		[]string{"repository", "runner_name"},
	)
	mu sync.Mutex
)

func init() {
	// Register Prometheus metrics
	prometheus.MustRegister(
		workflowRunTotal,
		workflowDurationSeconds,
		jobRunTotal,
		jobDurationSeconds,
		stepRunTotal,
		stepDurationSeconds,
		queuedDurationSeconds,
		runnersBusy,
	)
}

func webhookHandler(c *gin.Context) {
	fmt.Println("\n\n\n\n Running Webhook")
	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	//fmt.Println("Raw Payload: ", string(body))
	var payload GitHubWebhookPayload
	//var payload2 GitHubWebhookPayload

	err = json.Unmarshal(body, &payload)
	if err != nil {
		fmt.Println("\n\n error in unmarshal json : ", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	//fmt.Println("payload: ", payload)

	mu.Lock()
	defer mu.Unlock()

	if payload.Action == "queued" {
		fmt.Printf(" Action is in Queued :  workflow_job.id  %v , run_id %v ,status %s ,name %s ,Repo name %v",
			payload.WorkflowJob.ID,
			payload.WorkflowJob.RunID,
			payload.WorkflowJob.Status,
			payload.WorkflowJob.Name,
			payload.Repository.FullName,
		)
		duration := payload.WorkflowJob.CompletedAt.Sub(payload.WorkflowJob.StartedAt).Seconds()
		queuedDurationSeconds.WithLabelValues(payload.Repository.FullName, payload.WorkflowJob.Name).Observe(duration)
	}
	if payload.Action == "in_progress" {
		fmt.Printf(" Action is in in_progress :  workflow_job.id  %v , run_id %v ,name %s ,Repo name %s",
			payload.WorkflowJob.ID,
			payload.WorkflowJob.RunID,
			//payload.WorkflowJob.Status,
			payload.WorkflowJob.Name,
			payload.Repository.FullName,
		)
		for _, step := range payload.WorkflowJob.Steps {
			fmt.Printf("  Step: %s | Status: %s | Number: %d\n",
				step.Name, step.Status, step.Number)
		}
	}
	if payload.Action == "completed" {
		fmt.Printf(" Action is in completed :  workflow_job.id  %v , run_id %v ,name %s ,Repo name %s",
			payload.WorkflowJob.ID,
			payload.WorkflowJob.RunID,
			payload.WorkflowJob.Name,
			payload.Repository.FullName,
		)
		for _, step := range payload.WorkflowJob.Steps {
			fmt.Printf("  Step: %s | Status: %s | Conclusion %s | started_at %v | Completed_at: %d\n",
				step.Name, step.Status, step.Conclusion, step.StartedAt, step.CompletedAt)
		}

		fmt.Printf("\nWorkflowRun Job ID: %d, Run ID: %d, Name: %s, Status: %s, Conclusion %s, StartAt %s UpdatedAt %s Repo: %s\n",
			payload.WorkflowRun.ID,
			payload.WorkflowRun.RunNumber,
			payload.WorkflowRun.Name,
			payload.WorkflowRun.Status,
			payload.WorkflowRun.Conclusion,
			payload.WorkflowRun.StartedAt,
			payload.WorkflowRun.UpdatedAt,
			payload.Repository.FullName,
		)
	}

	if payload.WorkflowJob.ID != 0 {
		fmt.Printf("\nJob ID: %d, Run ID: %d, Name: %s, Status: %s, Repo: %s\n",
			payload.WorkflowJob.ID,
			payload.WorkflowJob.RunID,
			payload.WorkflowJob.Name,
			payload.WorkflowJob.Status,
			payload.Repository.FullName,
		)

		if len(payload.WorkflowJob.Steps) > 0 {
			for _, step := range payload.WorkflowJob.Steps {
				fmt.Printf("  Step: %s | Status: %s | Conclusion: %s | Number: %d\n",
					step.Name, step.Status, step.Conclusion, step.Number)
			}
		}

		job := payload.WorkflowJob
		jobName := job.Name
		workflow := jobName // Approximate workflow name
		status := job.Conclusion

		if payload.Action == "completed" {
			jobRunTotal.WithLabelValues(payload.Repository.FullName, workflow, jobName, status).Inc()

			duration := job.CompletedAt.Sub(job.StartedAt).Seconds()
			jobDurationSeconds.WithLabelValues(payload.Repository.FullName, workflow, jobName, status).Observe(duration)
		}

		if payload.Action == "in_progress" {
			runnersBusy.WithLabelValues(payload.Repository.FullName, jobName).Set(1)
		} else if payload.Action == "completed" {
			runnersBusy.WithLabelValues(payload.Repository.FullName, jobName).Set(0)
		}

		for _, step := range job.Steps {
			stepRunTotal.WithLabelValues(payload.Repository.FullName, workflow, jobName, step.Name, step.Status).Inc()
			if payload.Action == "completed" {
				duration := step.CompletedAt.Sub(step.StartedAt).Seconds()
				stepDurationSeconds.WithLabelValues(payload.Repository.FullName, workflow, jobName, step.Name, step.Status).Observe(duration)
			}
		}
	}
	if payload.WorkflowRun.ID != 0 {
		fmt.Printf("\nJob ID: %d, Run ID: %d, Name: %s, Status: %s, Conclusion %s, StartAt %s UpdatedAt %s Repo: %s\n",
			payload.WorkflowRun.ID,
			payload.WorkflowRun.RunNumber,
			payload.WorkflowRun.Name,
			payload.WorkflowRun.Status,
			payload.WorkflowRun.Conclusion,
			payload.WorkflowRun.StartedAt,
			payload.WorkflowRun.UpdatedAt,
			payload.Repository.FullName,
		)

		run := payload.WorkflowRun
		workflow := run.Name
		//status := run.Conclusion

		if payload.Action == "completed" {
			workflowRunTotal.WithLabelValues(payload.Repository.FullName, workflow, run.Status).Inc()

			duration := run.UpdatedAt.Sub(run.StartedAt).Seconds()
			fmt.Println("duration : ", duration)
			workflowDurationSeconds.WithLabelValues(payload.Repository.FullName, workflow, run.Status).Observe(duration)
		}
	}
	c.String(http.StatusOK, "Event processed")
}

func metricsHandler(c *gin.Context) {
	// Use promhttp to expose metrics
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
}

func main() {
	r := gin.Default()

	r.POST("/webhook", webhookHandler)
	r.GET("/metrics", metricsHandler)
	r.GET("/readyness", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	r.Run(":8080") // listen and serve on 0.0.0.0:8080
}
