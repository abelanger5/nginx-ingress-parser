package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/honeycombio/gonx"
)

// nginx's default log format
const defaultLogFormat = `$remote_addr - $remote_user [$time_local] "$request" $status $bytes_sent "$http_referer" "$http_user_agent" "$http_x_forwarded_for"`

// envoy's default log format
// https://envoyproxy.github.io/envoy/configuration/http_conn_man/access_log.html#config-http-con-manager-access-log-default-format
const envoyLogFormat = `[$timestamp] "$request" $status_code $response_flags $bytes_received $bytes_sent $duration $x_envoy_upstream_service_time "$x_forwarded_for" "$user_agent" "$x_request_id" "$authority" "$upstream_host"`

// nginx ingress default log format
// https://github.com/kubernetes/ingress-nginx/blob/9c6201b79a8b4/internal/ingress/controller/config/config.go#L53
const nginxIngressLogFormat = `$the_real_ip - $remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent" $request_length $request_time [$proxy_upstream_name] $upstream_addr $upstream_response_length $upstream_response_time $upstream_status $req_id`

type NginxParserFactory struct {
	parserName string
	logFormat  string
}

func (pf *NginxParserFactory) Init(options map[string]interface{}) error {
	pf.logFormat = `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent" $request_length $request_time [$proxy_upstream_name] [$proxy_alternative_upstream_name] $upstream_addr $upstream_response_length $upstream_response_time $upstream_status $req_id`

	return nil
}

func (pf *NginxParserFactory) New() Parser {
	return &NginxParser{
		gonxParser: gonx.NewParser(pf.logFormat),
	}
}

type NginxParser struct {
	gonxParser *gonx.Parser
}

// This is basically lifted from honeytail

func (p *NginxParser) Parse(line string) (map[string]interface{}, error) {
	gonxEvent, err := p.gonxParser.ParseString(line)

	if err != nil {
		return nil, err
	}

	return typeifyParsedLine(gonxEvent.Fields), nil
}

// typeifyParsedLine attempts to cast numbers in the event to floats or ints
func typeifyParsedLine(pl map[string]string) map[string]interface{} {
	// try to convert numbers, if possible
	msi := make(map[string]interface{}, len(pl))
	for k, v := range pl {
		switch {
		case strings.Contains(v, "."):
			f, err := strconv.ParseFloat(v, 64)
			if err == nil {
				msi[k] = f
				continue
			}
		case v == "-":
			// no value, don't set a "-" string
			continue
		default:
			i, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				msi[k] = i
				continue
			}
		}
		msi[k] = v
	}
	return msi
}

type LatencyMetric struct {
	latency float64
	time    time.Time
}

type LatencyMetricList struct {
	IP        string
	Latencies []*LatencyMetric
}

type Metric struct {
	buckets   map[string]*LatencyMetricList
	numOver2s int
}

func (m *Metric) AddLine(parsedLine map[string]interface{}, rawLine string) {
	if m.buckets == nil {
		m.buckets = map[string]*LatencyMetricList{}
	}

	upstreamAddr, ok := parsedLine["upstream_addr"].(string)

	if !ok {
		return
	}

	reqLatency, ok := parsedLine["request_time"].(float64)

	if !ok {
		fmt.Println("could not find or cast request time")
		return
	}

	reqTimeStr, ok := parsedLine["time_local"].(string)

	if !ok {
		return
	}

	layout := "2/Jan/2006:15:04:05 +0000"
	t, err := time.Parse(layout, reqTimeStr)

	if err != nil {
		fmt.Println("ERROR IS", err)
		return
	}

	if reqLatency > 10 {
		// fmt.Println(upstreamAddr, reqLatency, t, rawLine)

		// err := beeep.Alert("Request > 10 seconds", "That's bad.", "assets/warning.png")

		// if err != nil {
		// 	panic(err)
		// }
		m.numOver2s++
	}

	bucket, exists := m.buckets[upstreamAddr]

	if !exists {
		bucket = &LatencyMetricList{
			IP:        upstreamAddr,
			Latencies: make([]*LatencyMetric, 0),
		}

		m.buckets[upstreamAddr] = bucket
	}

	bucket.Latencies = append(bucket.Latencies, &LatencyMetric{
		latency: reqLatency,
		time:    t,
	})

	return
}

func (m *Metric) GetInfo() {
	fmt.Println("number of pods listed:", len(m.buckets))

	countReqs := 0

	for _, bucket := range m.buckets {
		countReqs += len(bucket.Latencies)
	}

	fmt.Println("number of requests listed:", countReqs)

	fmt.Printf("number of requests over 2 seconds: %d %.4f\n", m.numOver2s, 100*float64(m.numOver2s)/float64(countReqs))
}

func (m *Metric) WriteToCSV() {
	data := make([][]string, 0)

	for _, bucket := range m.buckets {
		for _, latency := range bucket.Latencies {
			data = append(data, []string{bucket.IP, latency.time.String(), fmt.Sprintf("%f", latency.latency)})
		}
	}

	file, err := os.Create("results-all.csv")
	checkError("Cannot create file", err)
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	for _, value := range data {
		err := writer.Write(value)
		checkError("Cannot write to file", err)
	}

	dataGreater10s := make([][]string, 0)

	for _, bucket := range m.buckets {
		for _, latency := range bucket.Latencies {
			if latency.latency > 2 {
				dataGreater10s = append(dataGreater10s, []string{bucket.IP, latency.time.String(), fmt.Sprintf("%f", latency.latency)})
			}
		}
	}

	file, err = os.Create("results-greater-2s.csv")
	checkError("Cannot create file", err)
	defer file.Close()

	writer = csv.NewWriter(file)
	defer writer.Flush()

	for _, value := range dataGreater10s {
		err := writer.Write(value)
		checkError("Cannot write to file", err)
	}
}

func checkError(message string, err error) {
	if err != nil {
		log.Fatal(message, err)
	}
}

func main() {
	factory := &NginxParserFactory{}

	factory.Init(map[string]interface{}{})
	parser := factory.New()
	metric := &Metric{}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			metric.GetInfo()
			metric.WriteToCSV()
			os.Exit(0)
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		text := scanner.Text()
		res, err := parser.Parse(text)

		if err != nil {
			fmt.Errorf(err.Error())
		}

		metric.AddLine(res, text)
	}

	if err := scanner.Err(); err != nil {
		fmt.Println(err)
	}

	metric.GetInfo()
	metric.WriteToCSV()

	// http.HandleFunc("/", httpserver)
	// http.ListenAndServe(":8081", nil)
}

type Parser interface {
	Parse(line string) (map[string]interface{}, error)
}

// // generate random data for line chart
// func generateLineItems() []opts.LineData {
// 	items := make([]opts.LineData, 0)
// 	for i := 0; i < 7; i++ {
// 		items = append(items, opts.LineData{Value: rand.Intn(300)})
// 	}
// 	return items
// }

// func generateBarItems() []opts.BarData {
// 	items := make([]opts.BarData, 0)

// 	items = append(items, opts.BarData{Name: "4.9", Value: rand.Intn(300)})

// 	return items
// }

// func getXAxisData() []string {
// 	res := make([]string, 0)

// 	for i := 0.1; i < 10; i += 0.1 {
// 		res = append(res, fmt.Sprintf("%.1f", i))
// 	}

// 	return res
// }

// func bucketsToMap() map[string]int {
// }

// func addLatencyToBucket(buckets []int, latency float64) {
// }

// func httpserver(w http.ResponseWriter, _ *http.Request) {
// 	bar := charts.NewBar()
// 	// set some global options like Title/Legend/ToolTip or anything else
// 	bar.SetGlobalOptions(charts.WithTitleOpts(opts.Title{
// 		Title:    "My first bar chart generated by go-echarts",
// 		Subtitle: "It's extremely easy to use, right?",
// 	}))

// 	// Put data into instance
// 	bar.SetXAxis(getXAxisData()).AddSeries("Category A", generateBarItems())

// 	bar.Render(w)
// }
