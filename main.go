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

// Prometheus metrics
var (
	runningGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "github_actions_running_total",
			Help: "Number of workflows currently running",
		},
		[]string{"workflow_name"},
	)
	successCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_success_total",
			Help: "Total number of successful workflow runs",
		},
		[]string{"workflow_name"},
	)
	failureCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "github_actions_failure_total",
			Help: "Total number of failed workflow runs",
		},
		[]string{"workflow_name"},
	)
	mu sync.Mutex // To safely update metrics
)

func init() {
	// Register Prometheus metrics
	prometheus.MustRegister(runningGauge)
	prometheus.MustRegister(successCounter)
	prometheus.MustRegister(failureCounter)
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
	fmt.Println("payload: ", payload)

	if payload.Action == "queued" {
		fmt.Printf(" Action is in Queued :  workflow_job.id  %v , run_id %v ,status %s ,name %s ,Repo name %v",
			payload.WorkflowJob.ID,
			payload.WorkflowJob.RunID,
			payload.WorkflowJob.Status,
			payload.WorkflowJob.Name,
			payload.Repository.FullName,
		)
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
		fmt.Printf(" Action is in completed :  workflow_job.id  %v , run_id %v ,status %s ,name %s ,Repo name %s",
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

		//if len(payload.WorkflowJob.Steps) > 0 {
		//	for _, step := range payload.WorkflowJob.Steps {
		//		fmt.Printf("  Step: %s | Status: %s | Conclusion: %s | Number: %d\n",
		//			step.Name, step.Status, step.Conclusion, step.Number)
		//	}
		//}
	}
	//fmt.Println("\n\n Payload2 unmarshal : payload2.Action : ", payload2.Action, " payload2.WorkflowRun : ", payload2.WorkflowRun, " payload2.WorkflowJob : ", payload2.WorkflowJob)
	// Decode the incoming JSON payload
	//if err := c.ShouldBindJSON(&payload); err != nil {
	//	fmt.Println("error in payload binding: ", err)
	//	c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
	//	return
	//}
	fmt.Println("\n\npayload : ", payload)
	workflowName := payload.WorkflowRun.Name

	mu.Lock()
	defer mu.Unlock()
	fmt.Println("payload : ", payload)
	switch payload.WorkflowRun.Status {
	case "in_progress":
		// Workflow started running
		runningGauge.WithLabelValues(workflowName).Inc()

	case "completed":
		// Workflow completed: Decrement running
		runningGauge.WithLabelValues(workflowName).Dec()

		if payload.WorkflowRun.Conclusion == "success" {
			successCounter.WithLabelValues(workflowName).Inc()
		} else if payload.WorkflowRun.Conclusion == "failure" {
			failureCounter.WithLabelValues(workflowName).Inc()
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
