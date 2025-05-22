package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
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
	ID         int       `json:"id"`
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
	ID           int       `json:"id"`
	RunID        int       `json:"run_id"`
	Name         string    `json:"name"`
	WorkFlowName string    `json:"workflow_name"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	Steps        []Step    `json:"steps,omitempty"`
}

type GitHubWebhookPayload struct {
	Action      string      `json:"action"`
	WorkflowRun WorkflowRun `json:"workflow_run,omitempty"`
	WorkflowJob WorkflowJob `json:"workflow_job,omitempty"`
	Repository  Repository  `json:"repository,omitempty"`
}
type RunInfo struct {
	RunNumber int
	TimeStamp time.Time
}

var (
	workflowStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_actions_workflow_status",
			Help: "Latest status of workflow runs (1=active, 0=inactive)",
		},
		[]string{"id", "workflow", "repository"},
	)

	workflowRunTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_workflow_run_count",
			Help: "Total number of workflow runs per repository",
		},
		[]string{"repository", "workflow"},
	)
	//github_actions_workflow_run_count_success
	workflowRunCountSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_workflow_run_count_success",
			Help: "Total number of success workflow runs",
		},
		[]string{"repository", "workflow"},
	)
	workflowRunCountFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_workflow_run_count_failed",
			Help: "Total number of failed workflow runs",
		},
		[]string{"repository", "workflow"},
	)

	workflowRunDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_actions_workflow_run_duration",
			Help: "Duration of the workflow run in seconds",
		},
		[]string{"id", "workflow", "repository"},
	)

	jobRunTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_job_run_total",
			Help: "Total number of jobs run within workflows",
		},
		[]string{"repository", "workflow", "job", "status"},
	)

	jobStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_actions_job_status",
			Help: "Latest status of a job (1=active, 0=inactive)",
		},
		[]string{"id", "workflow", "job_name", "repository"},
	)

	jobDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_actions_job_run_duration",
			Help: "Job run duration in seconds",
		},
		[]string{"id", "workflow", "job_name", "repository"},
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

	runIDCache = make(map[int]RunInfo)
	cacheMu    sync.Mutex
	cacheTTL   = 30 * time.Minute
)

func init() {
	// Register Prometheus metrics
	prometheus.MustRegister(
		workflowStatus,
		workflowRunTotal,
		workflowRunCountSuccess,
		workflowRunCountFailed,
		workflowRunDuration,
		jobRunTotal,
		jobDuration,
		stepRunTotal,
		stepDurationSeconds,
		queuedDurationSeconds,
		runnersBusy,
		jobStatus,
	)
	go cacheCleaner()
}
func cacheCleaner() {
	for {
		time.Sleep(10 * time.Minute)
		cacheMu.Lock()
		now := time.Now()
		for k, v := range runIDCache {
			if now.Sub(v.TimeStamp) > cacheTTL {
				fmt.Println("deleting cache : ", runIDCache)
				delete(runIDCache, k)
			}
		}
		cacheMu.Unlock()
	}
}

type ForRunNumber struct {
	Id        int    `json:"id"`
	RunNumber string `json:"run_number"`
}

func fetchRunNumber(runID int, token string) (ForRunNumber, error) {
	url := fmt.Sprintf("https://api.github.com/repos/Abhishek-Valaboju/Github-events/actions/runs/%d", runID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return ForRunNumber{}, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "go-github-client")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return ForRunNumber{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ForRunNumber{}, fmt.Errorf("GitHub API error: %s\n%s", resp.Status, string(body))
	}

	var run ForRunNumber
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return ForRunNumber{}, err
	}

	return run, nil
}
func webhookHandler(c *gin.Context) {
	fmt.Println("\n\n\n\n Running Webhook")
	body, err := ioutil.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fmt.Println("Raw Payload: ", string(body))
	var payload GitHubWebhookPayload

	err = json.Unmarshal(body, &payload)
	if err != nil {
		fmt.Println("\n\n error in unmarshal json : ", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	mu.Lock()
	defer mu.Unlock()
	fmt.Println("updated at : ", payload.WorkflowRun.UpdatedAt, "completed at : ", payload.WorkflowJob.CompletedAt)

	cutoff := time.Now().Add(-7 * 24 * time.Hour)

	if (payload.WorkflowJob.ID != 0 && !payload.WorkflowJob.CompletedAt.IsZero() && payload.WorkflowJob.CompletedAt.Before(cutoff)) ||
		(payload.WorkflowRun.ID != 0 && !payload.WorkflowRun.UpdatedAt.IsZero() && payload.WorkflowRun.UpdatedAt.Before(cutoff)) {
		fmt.Println("Discarding WorkflowJob event older than 7 days. CompletedAt : ", payload.WorkflowJob.CompletedAt, " updated at : ", payload.WorkflowRun.UpdatedAt)
		c.Status(http.StatusNoContent)
		return
	}

	if payload.WorkflowRun.ID != 0 {
		cacheMu.Lock()
		runIDCache[payload.WorkflowRun.ID] = RunInfo{
			RunNumber: payload.WorkflowRun.RunNumber,
			TimeStamp: time.Now(),
		}
		cacheMu.Unlock()
	}

	var runNumber int

	cacheMu.Lock()
	if info, ok := runIDCache[payload.WorkflowJob.RunID]; ok {
		runNumber = info.RunNumber
		fmt.Println("runNumber : ", runNumber, " runID : ", payload.WorkflowJob.RunID)
	} else {
		runNumber = payload.WorkflowJob.RunID
	}
	cacheMu.Unlock()
	run_number, err := fetchRunNumber(payload.WorkflowJob.RunID, "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")
	if err != nil {
		fmt.Println("Error fetching run number : ", err)
	}
	fmt.Println("run_number : ", run_number)
	//if payload.Action == "queued" {
	//	fmt.Printf(" Action is in Queued :  workflow_job.id  %v , run_id %v ,status %s ,name %s ,Repo name %v",
	//		payload.WorkflowJob.ID,
	//		payload.WorkflowJob.RunID,
	//		payload.WorkflowJob.Status,
	//		payload.WorkflowJob.Name,
	//		payload.Repository.FullName,
	//	)
	//	duration := payload.WorkflowJob.CompletedAt.Sub(payload.WorkflowJob.StartedAt).Seconds()
	//	queuedDurationSeconds.WithLabelValues(payload.Repository.FullName, strconv.Itoa(payload.WorkflowJob.RunID)).Observe(duration)
	//}
	//if payload.Action == "in_progress" {
	//	fmt.Printf(" Action is in in_progress :  workflow_job.id  %v , run_id %v ,name %s ,Repo name %s",
	//		payload.WorkflowJob.ID,
	//		payload.WorkflowJob.RunID,
	//		//payload.WorkflowJob.Status,
	//		payload.WorkflowJob.Name,
	//		payload.Repository.FullName,
	//	)
	//	for _, step := range payload.WorkflowJob.Steps {
	//		fmt.Printf("  Step: %s | Status: %s | Number: %d\n",
	//			step.Name, step.Status, step.Number)
	//	}
	//}

	//if payload.Action == "completed" {
	//	fmt.Printf(" Action is in completed :  workflow_job.id  %v , run_id %v ,name %s ,Repo name %s",
	//		payload.WorkflowJob.ID,
	//		payload.WorkflowJob.RunID,
	//		payload.WorkflowJob.Name,
	//		payload.Repository.FullName,
	//	)
	//	for _, step := range payload.WorkflowJob.Steps {
	//		fmt.Printf("  Step: %s | Status: %s | Conclusion %s | started_at %v | Completed_at: %d\n",
	//			step.Name, step.Status, step.Conclusion, step.StartedAt, step.CompletedAt)
	//	}
	//
	//	fmt.Printf("\nWorkflowRun Job ID: %d, Run ID: %d, Name: %s, Status: %s, Conclusion %s, StartAt %s UpdatedAt %s Repo: %s\n",
	//		payload.WorkflowRun.ID,
	//		payload.WorkflowRun.RunNumber,
	//		payload.WorkflowRun.Name,
	//		payload.WorkflowRun.Status,
	//		payload.WorkflowRun.Conclusion,
	//		payload.WorkflowRun.StartedAt,
	//		payload.WorkflowRun.UpdatedAt,
	//		payload.Repository.FullName,
	//	)
	//}

	//sending metrics
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
		jobstatus := job.Conclusion
		workFlowName := job.WorkFlowName
		jobRunTotal.WithLabelValues(payload.Repository.FullName, strconv.Itoa(runNumber), job.Name, jobstatus).Inc()

		if payload.Action == "completed" {
			duration := job.CompletedAt.Sub(job.StartedAt).Seconds()
			jobDuration.WithLabelValues(strconv.Itoa(job.ID), strconv.Itoa(runNumber), job.Name, payload.Repository.FullName).Set(duration)
		}

		if payload.Action == "in_progress" {
			runnersBusy.WithLabelValues(payload.Repository.FullName, job.Name).Set(1)
		} else if payload.Action == "completed" {
			runnersBusy.WithLabelValues(payload.Repository.FullName, job.Name).Set(0)
		}

		status := 0.0

		if payload.Action == "queued" {
			status = 0.0
		} else if payload.Action == "in_progress" {
			status = 0.5
		} else if payload.Action == "completed" {
			if jobstatus == "success" {
				status = 1.0
			} else if jobstatus == "failure" {
				status = 2.0
			}
		}

		jobStatus.WithLabelValues(strconv.Itoa(runNumber), workFlowName, job.Name, payload.Repository.FullName).Set(status)

		for _, step := range job.Steps {
			stepRunTotal.WithLabelValues(payload.Repository.FullName, strconv.Itoa(runNumber), job.Name, step.Name, step.Status).Inc()
			if payload.Action == "completed" {
				duration := step.CompletedAt.Sub(step.StartedAt).Seconds()
				stepDurationSeconds.WithLabelValues(payload.Repository.FullName, strconv.Itoa(runNumber), job.Name, step.Name, step.Conclusion).Observe(duration)
			}
		}
	}

	if payload.WorkflowRun.ID != 0 {
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

		run := payload.WorkflowRun
		runWorkflow := run.RunNumber
		runStatus := run.Conclusion

		//workflow run total count
		workflowRunTotal.WithLabelValues(payload.Repository.FullName, strconv.Itoa(runWorkflow)).Inc()

		//workflow run success and failure counters
		if runStatus == "success" {
			//workflowRunCountSuccess.WithLabelValues(payload.Repository.FullName, strconv.Itoa(runWorkflow)).Inc()
			workflowRunCountSuccess.WithLabelValues(payload.Repository.FullName, run.Name).Inc()
		} else if runStatus == "failure" {
			workflowRunCountFailed.WithLabelValues(payload.Repository.FullName, run.Name).Inc()
		}

		if payload.Action == "completed" {
			duration := run.UpdatedAt.Sub(run.StartedAt).Seconds()
			fmt.Println("duration : ", duration)
			workflowRunDuration.WithLabelValues(strconv.Itoa(run.ID), strconv.Itoa(runWorkflow), payload.Repository.FullName).Set(duration)
		}

		status := 0.0

		if payload.Action == "requested" {
			status = 0.0
		} else if payload.Action == "in_progress" {
			status = 0.5
		} else if payload.Action == "completed" {
			if runStatus == "success" {
				status = 1.0
			} else if runStatus == "failure" {
				status = 2.0
			}
		}
		workflowStatus.WithLabelValues(strconv.Itoa(runWorkflow), run.Name, payload.Repository.FullName).Set(status)
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
	r.GET("/get_number", func(c *gin.Context) {
		if len(runIDCache) != 0 {
			for i, v := range runIDCache {
				fmt.Println("runID : ", i, "run Number : ", v)
				c.JSON(http.StatusOK, gin.H{"runID : ": i, " runNumber : ": v})
			}
		} else {
			c.Status(http.StatusNoContent)
		}
	})
	r.Run(":8080")
}
