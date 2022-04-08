package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"

	"github.com/abelanger5/nginx-ingress-parser/internal/metric"
	"github.com/abelanger5/nginx-ingress-parser/internal/parser"
	"github.com/spf13/cobra"
)

// wrap with cobra
var rootCmd = &cobra.Command{
	Use: "nginx-parser",
	Run: func(cmd *cobra.Command, args []string) {
		factory := &parser.NginxParserFactory{}

		factory.Init(map[string]interface{}{})
		parser := factory.New()
		collector := metric.NewMetricCollector(metric.GroupKindPath, metric.MetricKindLatency)

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		go func() {
			for range c {
				collector.GetInfo()
				os.Exit(0)
			}
		}()

		scanner := bufio.NewScanner(os.Stdin)

		for scanner.Scan() {
			text := scanner.Text()
			res, err := parser.Parse(text)

			if err != nil {
				continue
			}

			collector.AddLine(res, text)
		}

		if err := scanner.Err(); err != nil {
			fmt.Println(err)
		}

		collector.GetInfo()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func main() {
	Execute()
}
