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
	intervaSecond := flag.Int("interval-millli-second", 60, "interval sec")
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

	modelLabels := getResourceLabelsForModel(*podID, *namespace, *podName)
	for {
		resp, err := http.Get("http://localhost:3000/server-status?json")
		if err != nil {
			log.Println(err.Error())
			time.Sleep(time.Duration(*intervaSecond) * time.Second)
			continue
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println(err.Error())
			time.Sleep(time.Duration(*intervaSecond) * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			log.Printf("%s", body)
			time.Sleep(time.Duration(*intervaSecond) * time.Second)
			continue
		}
		output := ServerStatus{}
		err = json.Unmarshal(body, &output)
		if err != nil {
			log.Printf(err.Error())
			time.Sleep(time.Duration(*intervaSecond) * time.Second)
			continue
		}
		// https://cloud.google.com/kubernetes-engine/docs/tutorials/custom-metrics-autoscaling#step2
		busyErr := exportMetric(stackdriverService, busyWorkerNumMetricName, output.BusyWorkers, "gke_container", modelLabels)
		if busyErr != nil {
			log.Printf("Failed to write time series data for new resource model: %v\n", busyErr)
		}
		idleErr := exportMetric(stackdriverService, idleWorkerNumMetricName, output.IdleWorkers, "gke_container", modelLabels)
		if idleErr != nil {
			log.Printf("Failed to write time series data for new resource model: %v\n", idleErr)
		}
		time.Sleep(time.Duration(*intervaSecond) * time.Second)
	}
}

func getStackDriverService() (*monitoring.Service, error) {
	oauthClient := oauth2.NewClient(context.Background(), google.ComputeTokenSource(""))
	return monitoring.New(oauthClient)
}

// getResourceLabelsForNewModel returns resource labels needed to correctly label metric data
// exported to StackDriver. Labels contain details on the cluster (project id, location, name)
// and pod for which the metric is exported (namespace, name).
func getResourceLabelsForModel(podID, namespace, name string) map[string]string {
	projectID, _ := gce.ProjectID()
	zone, _ := gce.Zone()
	location, _ := gce.InstanceAttributeValue("cluster-location")
	location = strings.TrimSpace(location)
	clusterName, _ := gce.InstanceAttributeValue("cluster-name")
	clusterName = strings.TrimSpace(clusterName)
	return map[string]string{
		"project_id":   projectID,
		"zone":         zone,
		"cluster_name": clusterName,
		// container name doesn't matter here, because the metric is exported for
		// the pod, not the container
		"container_name": "",
		"pod_id":         podID,
		// namespace_id and instance_id don't matter
		"namespace_id": namespace,
		"instance_id":  "",
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
