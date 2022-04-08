package metric

import (
	"fmt"
	"time"

	"github.com/abelanger5/nginx-ingress-parser/internal/parser"
)

type MetricKind string

const (
	MetricKindLatency      MetricKind = "latency"
	MetricKindResponseCode MetricKind = "response_code"
)

type GroupKind string

const (
	GroupKindUpstreamIP GroupKind = "upstream_ip"
	GroupKindPath       GroupKind = "path"
)

type LatencyMetric struct {
	latency float64
	time    time.Time
}

type LatencyMetricList struct {
	IP        string
	Latencies []*LatencyMetric
}

type ResponseMetric map[int64]uint

type TimedOutMetric struct {
	Count int
	Total int
}

type MetricCollector struct {
	group        GroupKind
	metric       MetricKind
	latencyData  map[string]*LatencyMetricList
	responseData map[string]ResponseMetric
	timedOutData map[string]TimedOutMetric
}

func NewMetricCollector(group GroupKind, metric MetricKind) *MetricCollector {
	return &MetricCollector{group, metric, nil, nil, nil}
}

func (m *MetricCollector) AddLine(result *parser.NginxResult, rawLine string) {
	if result == nil {
		return
	}

	if m.latencyData == nil {
		m.latencyData = make(map[string]*LatencyMetricList)
	}

	if m.timedOutData == nil {
		m.timedOutData = make(map[string]TimedOutMetric)
	}

	if m.responseData == nil {
		m.responseData = make(map[string]ResponseMetric)
	}

	// TODO: figure out which field to group by
	if result.Request == nil {
		return
	}

	group := result.Request.Path

	// only include in latency data if it didn't time out
	if !result.TimedOut {
		bucket, exists := m.latencyData[group]

		if !exists {
			bucket = &LatencyMetricList{
				IP:        result.UpstreamAddr,
				Latencies: make([]*LatencyMetric, 0),
			}

			m.latencyData[group] = bucket
		}

		bucket.Latencies = append(bucket.Latencies, &LatencyMetric{
			latency: result.RequestTime,
			time:    result.TimeLocal,
		})
	}

	respBucket, exists := m.responseData[group]

	if !exists {
		respBucket = make(ResponseMetric)

		respBucket[result.UpstreamStatus] = 1
	} else {
		_, exists := respBucket[result.UpstreamStatus]

		if !exists {
			respBucket[result.UpstreamStatus] = 1
		} else {
			respBucket[result.UpstreamStatus]++
		}
	}

	m.responseData[group] = respBucket

	timedOutMetric, exists := m.timedOutData[group]

	if !exists {
		timedOutMetric = TimedOutMetric{}
	}

	timedOutMetric.Total++

	if result.TimedOut {
		timedOutMetric.Count++
	}

	m.timedOutData[group] = timedOutMetric

	return
}

func (m *MetricCollector) GetInfo() {
	// fmt.Println("number of pods listed:", len(m.latencyData))
	fmt.Printf(`
---------------------------------
OVERVIEW
---------------------------------	
`)

	countReqs := 0

	for _, bucket := range m.latencyData {
		countReqs += len(bucket.Latencies)
	}

	fmt.Println("Total number of requests tracked:", countReqs)

	fmt.Printf(`
---------------------------------
RESPONSE STATUS CODE METRICS
---------------------------------	
`)

	for path, bucket := range m.responseData {
		has4XXOr5XX := false
		var totReqs uint = 0

		for code, num := range bucket {
			has4XXOr5XX = has4XXOr5XX || (code >= 400)
			totReqs += num
		}

		if has4XXOr5XX && totReqs > 100 {
			fmt.Printf("%s:\n", path)

			for code, num := range bucket {
				fmt.Printf("  %d -- %d\n", code, num)
			}

			fmt.Printf("Total: %d \n\n", totReqs)
		}
	}

	fmt.Printf(`
---------------------------------
TIME OUT PERCENTAGES
---------------------------------	
`)

	for path, timedOutMetric := range m.timedOutData {
		if timedOutMetric.Count > 0 && timedOutMetric.Total > 100 {
			fmt.Printf("%s: %d / %d (%.2f%%)\n", path, timedOutMetric.Count, timedOutMetric.Total, 100.0*float64(timedOutMetric.Count)/float64(timedOutMetric.Total))
		}
	}

	numOver2s := 0

	for path, bucket := range m.latencyData {
		var totLatency float64 = 0
		var totReqs float64 = float64(len(bucket.Latencies))

		for _, latency := range bucket.Latencies {
			totLatency += latency.latency

			if latency.latency > 2000 {
				numOver2s++
			}
		}

		fmt.Printf("%s: %f (tot %.0f) \n", path, totLatency/totReqs, totReqs)
	}

	fmt.Printf("number of requests over 2 seconds: %d %.4f\n", numOver2s, 100*float64(numOver2s)/float64(countReqs))
}

// func (m *MetricCollector) WriteToCSV() {
// 	data := make([][]string, 0)

// 	for _, bucket := range m.latencyData {
// 		for _, latency := range bucket.Latencies {
// 			data = append(data, []string{bucket.IP, latency.time.String(), fmt.Sprintf("%f", latency.latency)})
// 		}
// 	}

// 	file, err := os.Create("results-all.csv")
// 	checkError("Cannot create file", err)
// 	defer file.Close()

// 	writer := csv.NewWriter(file)
// 	defer writer.Flush()

// 	for _, value := range data {
// 		err := writer.Write(value)
// 		checkError("Cannot write to file", err)
// 	}

// 	dataGreater10s := make([][]string, 0)

// 	for _, bucket := range m.buckets {
// 		for _, latency := range bucket.Latencies {
// 			if latency.latency > 2 {
// 				dataGreater10s = append(dataGreater10s, []string{bucket.IP, latency.time.String(), fmt.Sprintf("%f", latency.latency)})
// 			}
// 		}
// 	}

// 	file, err = os.Create("results-greater-2s.csv")
// 	checkError("Cannot create file", err)
// 	defer file.Close()

// 	writer = csv.NewWriter(file)
// 	defer writer.Flush()

// 	for _, value := range dataGreater10s {
// 		err := writer.Write(value)
// 		checkError("Cannot write to file", err)
// 	}
// }

// func checkError(message string, err error) {
// 	if err != nil {
// 		log.Fatal(message, err)
// 	}
// }
