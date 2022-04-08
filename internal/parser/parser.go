package parser

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/honeycombio/gonx"
)

type Parser interface {
	Parse(line string) (map[string]interface{}, error)
}

const nginxIngressLogFormat = `$remote_addr - $remote_user [$time_local] "$request" $status $body_bytes_sent "$http_referer" "$http_user_agent" $request_length $request_time [$proxy_upstream_name] [$proxy_alternative_upstream_name] $upstream_addr $upstream_response_length $upstream_response_time $upstream_status $req_id`
const nginxIngressErrorFormat = `$time_date $time_hms [$status] $code: $id $message, client: $upstream_addr, server: $proxy_upstream_name, request: "$request", upstream: "$upstream_full", host: "$host"`
const nginxIngressTimeFormat = `2/Jan/2006:15:04:05 +0000`

type NginxParserFactory struct {
	parserName   string
	logFormat    string
	errLogFormat string
}

func (pf *NginxParserFactory) Init(options map[string]interface{}) error {
	pf.logFormat = nginxIngressLogFormat
	pf.errLogFormat = nginxIngressErrorFormat

	return nil
}

func (pf *NginxParserFactory) New() *NginxParser {
	return &NginxParser{
		gonxParser:    gonx.NewParser(pf.logFormat),
		gonxErrParser: gonx.NewParser(pf.errLogFormat),
	}
}

type NginxParser struct {
	gonxParser    *gonx.Parser
	gonxErrParser *gonx.Parser
}

type NginxResult struct {
	RemoteAddr     string
	RemoteUser     string
	UpstreamAddr   string
	TimeLocal      time.Time
	Request        *Request
	RequestTime    float64
	UpstreamStatus int64
	TimedOut       bool
}

type Request struct {
	Method string
	Path   string
	Query  string
}

func (p *NginxParser) Parse(line string) (*NginxResult, error) {
	gonxEvent, err := p.gonxParser.ParseString(line)

	if err != nil {
		// attempt to parse to error line
		gonxEventErr, err := p.gonxErrParser.ParseString(line)

		if err != nil {
			return nil, err
		}

		res, err := parsedErrLineToResult(typeifyParsedLine(gonxEventErr.Fields))

		if err != nil {
			return nil, err
		}

		return res, nil
	}

	res, err := parsedLineToResult(typeifyParsedLine(gonxEvent.Fields))

	if err != nil {
		return nil, err
	}

	return res, nil
}

func parsedLineToResult(line map[string]interface{}) (*NginxResult, error) {
	res := &NginxResult{}
	var err error

	if res.UpstreamAddr, err = toString(line, "upstream_addr"); err != nil {
		res.UpstreamAddr = "0.0.0.0"
		// return nil, err
	}

	if res.RequestTime, err = toFloat64(line, "request_time"); err != nil {
		return nil, err
	}

	reqTimeLocalStr, err := toString(line, "time_local")

	if err != nil {
		return nil, err
	}

	res.TimeLocal, err = time.Parse(nginxIngressTimeFormat, reqTimeLocalStr)

	if err != nil {
		return nil, err
	}

	if res.UpstreamStatus, err = toInt64(line, "upstream_status"); err != nil {
		return nil, err
	}

	reqStr, err := toString(line, "request")

	if err != nil {
		return nil, err
	}

	res.Request, err = requestStringToReq(reqStr)

	if err != nil {
		return nil, err
	}

	return res, nil
}

func parsedErrLineToResult(line map[string]interface{}) (*NginxResult, error) {
	res := &NginxResult{
		UpstreamStatus: 504,
		TimedOut:       true,
	}

	var err error

	if res.UpstreamAddr, err = toString(line, "upstream_addr"); err != nil {
		res.UpstreamAddr = "0.0.0.0"
		// return nil, err
	}

	reqStr, err := toString(line, "request")

	if err != nil {
		return nil, err
	}

	res.Request, err = requestStringToReq(reqStr)

	if err != nil {
		return nil, err
	}

	return res, nil
}

func toString(parsedLine map[string]interface{}, field string) (string, error) {
	strInt, exists := parsedLine[field]

	if !exists {
		return "", fmt.Errorf("field %s does not exist", field)
	}

	str, ok := strInt.(string)

	if !ok {
		return "", fmt.Errorf("field %s could not be converted to string", field)
	}

	return str, nil
}

func toFloat64(parsedLine map[string]interface{}, field string) (float64, error) {
	strInt, exists := parsedLine[field]

	if !exists {
		return 0, fmt.Errorf("field %s does not exist", field)
	}

	res, ok := strInt.(float64)

	if !ok {
		return 0, fmt.Errorf("field %s could not be converted to float64", field)
	}

	return res, nil
}

func toInt64(parsedLine map[string]interface{}, field string) (int64, error) {
	strInt, exists := parsedLine[field]

	if !exists {
		return 0, fmt.Errorf("field %s does not exist", field)
	}

	res, ok := strInt.(int64)

	if !ok {
		return 0, fmt.Errorf("field %s could not be converted to uint, is %s", field, reflect.TypeOf(strInt))
	}

	return res, nil
}

func requestStringToReq(str string) (*Request, error) {
	strArr := strings.Split(str, " ")

	if len(strArr) != 3 {
		return nil, fmt.Errorf("incorrect format for %s", str)
	}

	urlRes, err := url.Parse(fmt.Sprintf("http://localhost%s", strArr[1]))

	if err != nil {
		return nil, err
	}

	return &Request{
		Method: strArr[0],
		Path:   urlRes.Path,
		Query:  urlRes.RawQuery,
	}, nil
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
