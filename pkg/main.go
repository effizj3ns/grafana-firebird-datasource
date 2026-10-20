package main

import (
	"os"

	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/effizj3ns/grafana-firebird-datasource/pkg/plugin"
)

func main() {
	ds := plugin.NewFirebirdDatasource()

	err := datasource.Serve(datasource.ServeOpts{
		QueryDataHandler:   ds,
		CheckHealthHandler: ds,
	})
	if err != nil {
		log.DefaultLogger.Error(err.Error())
		os.Exit(1)
	}
}
