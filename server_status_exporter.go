package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"strings"

	gce "cloud.google.com/go/compute/metadata"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	monitoring "google.golang.org/api/monitoring/v3"
)

// ServerStatus is rack-server_status response
type ServerStatus struct {
	Uptime      int   `json:"Uptime"`
	BusyWorkers int64 `json:"BusyWorkers"`
	IdleWorkers int64 `json:"IdleWorkers"`
	Stats       []struct {
		RemoteAddr interface{} `json:"remote_addr"`
		Host       string      `json:"host"`
		Method     interface{} `json:"method"`
		URI        interface{} `json:"uri"`
		Protocol   interface{} `json:"protocol"`
		Pid        int         `json:"pid"`
		Status     string      `json:"status"`
		Ss         int         `json:"ss"`
	} `json:"stats"`
}

func main() {
	podID := flag.String("pod-id", "", "pod id")
	namespace := flag.String("namespace", "", "namespace")
	podName := flag.String("pod-name", "", "pod name")
	intervalSecond := flag.Int("interval-milli-second", 60, "interval sec")
	busyWorkerNumMetricName := "busy-worker-num"
	idleWorkerNumMetricName := "idle-worker-num"
	flag.Parse()

	if *podID == "" {
		log.Fatalf("No pod id specified.")
	}

	if *podName == "" {
		log.Fatalf("No pod name specified.")
	}

	if *namespace == "" {
		log.Fatalf("No pod namespace specified.")
	}

	stackdriverService, err := getStackDriverService()
	if err != nil {
		log.Fatalf("Error getting Stackdriver service: %v", err)
	}

	modelLabels := getResourceLabelsForModel(*namespace, *podName)
	for {
		resp, err := http.Get("http://localhost:3000/server-status?json")
		if err != nil {
			log.Printf("request err: %v\n", err)
			time.Sleep(time.Duration(*intervalSecond) * time.Second)
			continue
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("body parse err: %v\n", err)
			time.Sleep(time.Duration(*intervalSecond) * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			log.Printf("server-status status error %s\n", body)
			time.Sleep(time.Duration(*intervalSecond) * time.Second)
			continue
		}
		output := ServerStatus{}
		err = json.Unmarshal(body, &output)
		if err != nil {
			log.Printf("json parse error %v\n", err)
			time.Sleep(time.Duration(*intervalSecond) * time.Second)
			continue
		}
		// https://cloud.google.com/kubernetes-engine/docs/tutorials/custom-metrics-autoscaling#step2
		busyErr := exportMetric(stackdriverService, busyWorkerNumMetricName, output.BusyWorkers, "k8s_pod", modelLabels)
		if busyErr != nil {
			log.Printf("Failed to write time series data for new resource model: %v\n", busyErr)
		}
		idleErr := exportMetric(stackdriverService, idleWorkerNumMetricName, output.IdleWorkers, "k8s_pod", modelLabels)
		if idleErr != nil {
			log.Printf("Failed to write time series data for new resource model: %v\n", idleErr)
		}
		time.Sleep(time.Duration(*intervalSecond) * time.Second)
	}
}

func getStackDriverService() (*monitoring.Service, error) {
	oauthClient := oauth2.NewClient(context.Background(), google.ComputeTokenSource(""))
	return monitoring.New(oauthClient)
}

// getResourceLabelsForNewModel returns resource labels needed to correctly label metric data
// exported to StackDriver. Labels contain details on the cluster (project id, location, name)
// and pod for which the metric is exported (namespace, name).
func getResourceLabelsForModel(namespace, name string) map[string]string {
	projectId, _ := gce.ProjectID()
	location, _ := gce.InstanceAttributeValue("cluster-location")
	location = strings.TrimSpace(location)
	clusterName, _ := gce.InstanceAttributeValue("cluster-name")
	clusterName = strings.TrimSpace(clusterName)
	return map[string]string{
		"project_id":     projectId,
		"location":       location,
		"cluster_name":   clusterName,
		"namespace_name": namespace,
		"pod_name":       name,
	}
}

func exportMetric(stackdriverService *monitoring.Service, metricName string,
	metricValue int64, monitoredResource string, resourceLabels map[string]string) error {
	dataPoint := &monitoring.Point{
		Interval: &monitoring.TimeInterval{
			EndTime: time.Now().Format(time.RFC3339),
		},
		Value: &monitoring.TypedValue{
			Int64Value: &metricValue,
		},
	}
	// Write time series data.
	request := &monitoring.CreateTimeSeriesRequest{
		TimeSeries: []*monitoring.TimeSeries{
			{
				Metric: &monitoring.Metric{
					Type: "custom.googleapis.com/" + metricName,
				},
				Resource: &monitoring.MonitoredResource{
					Type:   monitoredResource,
					Labels: resourceLabels,
				},
				Points: []*monitoring.Point{
					dataPoint,
				},
			},
		},
	}
	projectName := fmt.Sprintf("projects/%s", resourceLabels["project_id"])
	_, err := stackdriverService.Projects.TimeSeries.Create(projectName, request).Do()
	return err
}
