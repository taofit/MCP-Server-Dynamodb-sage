package server

import (
	"context"
	"dynamodb-sage/internal/metrics"
	"dynamodb-sage/internal/notification"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type tableInfo struct {
	TableName  string `json:"tableName"`
	ItemCount  int64  `json:"itemCount"`
	SizeBytes  int64  `json:"sizeBytes"`
	Status     string `json:"status"`
}

const maxTablesToDisplay = 100
const maxTableItemsToDisplay = 100

var extContentType = map[string]string{
	".js":   "application/javascript",
	".css":  "text/css",
	".html": "text/html; charset=utf-8",
	".png":  "image/png",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",
	".json": "application/json",
	".woff": "font/woff",
	".woff2": "font/woff2",
	".ttf": "font/ttf",
}

func ext(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i:]
	}
	return ""
}

//go:embed all:static/*
var dashboardFS embed.FS

func staticFileServer() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		staticFS, err := fs.Sub(dashboardFS, "static")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(staticFS, name)
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		isJS := strings.HasSuffix(name, ".js")
		isCSS := strings.HasSuffix(name, ".css")
		isHTML := strings.HasSuffix(name, ".html")

		if isJS {
			w.Header().Set("Content-Type", "application/javascript")
		} else if isCSS {
			w.Header().Set("Content-Type", "text/css")
		} else if isHTML {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}

		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
}

func (srv *Server) metricsProxy(w http.ResponseWriter, r *http.Request) {
	host := "localhost" + srv.metricsAddr
	resp, err := http.Get("http://" + host + "/metrics")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// send notification to all connected SSE clients
func (srv *Server) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ch := make(chan notification.NotificationPayload, 10)
	srv.sseClients.Store(r.Context(), ch)
	srv.connCount.Add(1)
	defer func() {
		srv.sseClients.Delete(r.Context())
		srv.connCount.Add(-1)
		close(ch)
	}()
	for {
		select {
		case n, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(n)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (srv *Server) prometheusServer() {
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(srv.metricsAddr, metricsMux)
}

func (srv *Server) health() map[string]string {
	return map[string]string{
		"dynamodb": srv.dbStatus(),
		"kafka":    srv.kafkaStatus(),
		"llm":      srv.llmStatus(),
	}
}

func (srv *Server) dbStatus() string {
	_, err := srv.db.ListTables(context.Background(), &dynamodb.ListTablesInput{})
	if err != nil {
		return "error"
	}
	return "ok"
}

func (srv *Server) kafkaStatus() string {
	if srv.kafkaClient == nil {
		return "not_configured"
	}
	if err := srv.kafkaClient.Ping(); err != nil {
		return "error"
	}
	return "ok"
}

func (srv *Server) llmStatus() string {
	if srv.llm == nil {
		return "not_configured"
	}
	if err := srv.llm.Ping(); err != nil {
		return "error"
	}
	return "ok"
}

func (srv *Server) stats() map[string]interface{} {
	notifications, err := srv.store.countNotifications()
	if err != nil {
		log.Printf("Warning: Failed to count notifications: %v", err)
	}
	chatMessages, err := srv.store.countChatMessages()
	if err != nil {
		log.Printf("Warning: Failed to count chat messages: %v", err)
	}
	numTables, err := srv.countTables()
	if err != nil {
		log.Printf("Warning: Failed to count tables: %v", err)
	}
	toolCalls := metrics.GetTotalToolInvocations()

	return map[string]interface{}{
		"active_connections": srv.connCount.Load(),
		"uptime_seconds":     time.Since(srv.startTime).Seconds(),
		"tables":             numTables,
		"chatMessages":       chatMessages,
		"notifications":      notifications,
		"toolCalls":          int(toolCalls),
	}
}

func (srv *Server) countTables() (int, error) {
	tableOutput, err := srv.db.ListTables(context.Background(), &dynamodb.ListTablesInput{})
	if err != nil {
		return 0, err
	}
	return len(tableOutput.TableNames), nil
}

func (srv *Server) listTablesInfo() ([]tableInfo, error) {
	tableNames, err := srv.fetchTableNames()
	if err != nil {
		return nil, err
	}
	tableInfos := []tableInfo{}
	total := maxTablesToDisplay
	if len(tableNames) < total {
		total = len(tableNames)
	}
	for i := 0; i < total; i++ {
		tableInfo, err := srv.getTableMetadata(tableNames[i])
		if err != nil {
			return nil, err
		}
		tableInfos = append(tableInfos, tableInfo)
	}
	return tableInfos, nil
}

func (srv *Server) fetchTableNames() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tableOutput, err := srv.db.ListTables(ctx, &dynamodb.ListTablesInput{})
	if err != nil {
		return nil, err
	}
	return tableOutput.TableNames, nil
}

func (srv *Server) getTableMetadata(tableName string) (tableInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	describeOutput, err := srv.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &tableName,
	})
	if err != nil {
		return tableInfo{}, err
	}
	tableInfo := tableInfo{
		TableName: *describeOutput.Table.TableName,
		ItemCount: *describeOutput.Table.ItemCount,
		SizeBytes: *describeOutput.Table.TableSizeBytes,
		Status:    string(describeOutput.Table.TableStatus),
	}
	return tableInfo, nil
}

type TableDescription struct {
	TableName            string                 `json:"tableName"`
	Status               string                 `json:"status"`
	KeySchema            []KeySchemaEntry       `json:"keySchema"`
	AttributeDefinitions []AttributeDefinitionDB `json:"attributeDefinitions"`
	ItemCount            int64                  `json:"itemCount"`
	SizeBytes            int64                  `json:"sizeBytes"`
	Throughput           *ThroughputInfo        `json:"throughput,omitempty"`
	BillingMode          string                 `json:"billingMode,omitempty"`
	GSIs                 []GSIInfo              `json:"gsis"`
	LSIs                 []GSIInfo              `json:"lsis"`
	TTLAttribute         string                 `json:"ttlAttribute,omitempty"`
	TTLEnabled           bool                   `json:"ttlEnabled"`
}

type KeySchemaEntry struct {
	AttributeName string `json:"attributeName"`
	KeyType       string `json:"keyType"`
}

type AttributeDefinitionDB struct {
	AttributeName string `json:"attributeName"`
	AttributeType string `json:"attributeType"`
}

type ThroughputInfo struct {
	ReadCapacityUnits  int64 `json:"readCapacityUnits"`
	WriteCapacityUnits int64 `json:"writeCapacityUnits"`
}

type GSIInfo struct {
	IndexName  string           `json:"indexName"`
	KeySchema  []KeySchemaEntry `json:"keySchema"`
	Projection *ProjectionInfo  `json:"projection,omitempty"`
	ItemCount  int64            `json:"itemCount"`
	SizeBytes  int64            `json:"sizeBytes"`
}

type ProjectionInfo struct {
	ProjectionType string `json:"projectionType"`
}

type secondaryIndex struct {
	IndexName      *string
	KeySchema      []types.KeySchemaElement
	Projection     *types.Projection
	ItemCount      *int64
	IndexSizeBytes *int64
}

// getTableInfo handles GET /api/tables/{tableName}
func (srv *Server) getTableInfo(w http.ResponseWriter, r *http.Request) {
	// Extract the tableName. Path starts with /api/tables/
	tableName := strings.TrimPrefix(r.URL.Path, "/api/tables/")
	if tableName == "" {
		http.Error(w, "missing table name", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Fetch table description from DynamoDB
	describeOutput, err := srv.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &tableName,
	})
	if err != nil {
		var rnfe *types.ResourceNotFoundException
		if errors.As(err, &rnfe) {
			http.Error(w, "table not found", http.StatusNotFound)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Fetch TTL details
	var ttlAttr string
	var ttlEnabled bool
	ttlOutput, err := srv.db.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{
		TableName: &tableName,
	})
	if err == nil && ttlOutput != nil && ttlOutput.TimeToLiveDescription != nil {
		if ttlOutput.TimeToLiveDescription.TimeToLiveStatus == types.TimeToLiveStatusEnabled {
			ttlEnabled = true
		}
		if ttlOutput.TimeToLiveDescription.AttributeName != nil {
			ttlAttr = *ttlOutput.TimeToLiveDescription.AttributeName
		}
	}

	// Map Key Schema
	keySchema := []KeySchemaEntry{}
	for _, k := range describeOutput.Table.KeySchema {
		keySchema = append(keySchema, KeySchemaEntry{
			AttributeName: *k.AttributeName,
			KeyType:       string(k.KeyType),
		})
	}

	// Map Attribute Definitions
	attrDefs := []AttributeDefinitionDB{}
	for _, a := range describeOutput.Table.AttributeDefinitions {
		attrDefs = append(attrDefs, AttributeDefinitionDB{
			AttributeName: *a.AttributeName,
			AttributeType: string(a.AttributeType),
		})
	}

	// Map GSIs
	gsis := []GSIInfo{}
	if describeOutput.Table.GlobalSecondaryIndexes != nil {
		for _, g := range describeOutput.Table.GlobalSecondaryIndexes {
			gsis = getSecondIndex(g, gsis)
		}
	}

	// Map LSIs
	lsis := []GSIInfo{}
	if describeOutput.Table.LocalSecondaryIndexes != nil {
		for _, l := range describeOutput.Table.LocalSecondaryIndexes {
			lsis = getSecondIndex(l, lsis)
		}
	}

	// Throughput
	var throughput *ThroughputInfo
	if describeOutput.Table.ProvisionedThroughput != nil {
		var readCap, writeCap int64
		if describeOutput.Table.ProvisionedThroughput.ReadCapacityUnits != nil {
			readCap = *describeOutput.Table.ProvisionedThroughput.ReadCapacityUnits
		}
		if describeOutput.Table.ProvisionedThroughput.WriteCapacityUnits != nil {
			writeCap = *describeOutput.Table.ProvisionedThroughput.WriteCapacityUnits
		}
		throughput = &ThroughputInfo{
			ReadCapacityUnits:  readCap,
			WriteCapacityUnits: writeCap,
		}
	}

	// Billing mode
	var billingMode string
	if describeOutput.Table.BillingModeSummary != nil {
		billingMode = string(describeOutput.Table.BillingModeSummary.BillingMode)
	} else if describeOutput.Table.ProvisionedThroughput != nil {
		billingMode = "PROVISIONED"
	}

	var tableItemCount int64
	if describeOutput.Table.ItemCount != nil {
		tableItemCount = *describeOutput.Table.ItemCount
	}
	var tableSizeBytes int64
	if describeOutput.Table.TableSizeBytes != nil {
		tableSizeBytes = *describeOutput.Table.TableSizeBytes
	}

	desc := TableDescription{
		TableName:            *describeOutput.Table.TableName,
		Status:               string(describeOutput.Table.TableStatus),
		KeySchema:            keySchema,
		AttributeDefinitions: attrDefs,
		ItemCount:            tableItemCount,
		SizeBytes:            tableSizeBytes,
		Throughput:           throughput,
		BillingMode:          billingMode,
		GSIs:                 gsis,
		LSIs:                 lsis,
		TTLAttribute:         ttlAttr,
		TTLEnabled:           ttlEnabled,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(desc)
}

func toSecondaryIndex(v any) secondaryIndex {
	switch i := v.(type) {
	case types.GlobalSecondaryIndexDescription:
		return secondaryIndex{i.IndexName, i.KeySchema, i.Projection, i.ItemCount, i.IndexSizeBytes}
	case types.LocalSecondaryIndexDescription:
		return secondaryIndex{i.IndexName, i.KeySchema, i.Projection, i.ItemCount, i.IndexSizeBytes}
	}
	return secondaryIndex{}
}

func getSecondIndex(v any, lsis []GSIInfo) []GSIInfo {
	l := toSecondaryIndex(v)
	lsiKeys := []KeySchemaEntry{}
	for _, k := range l.KeySchema {
		lsiKeys = append(lsiKeys, KeySchemaEntry{
			AttributeName: *k.AttributeName,
			KeyType:       string(k.KeyType),
		})
	}
	var proj *ProjectionInfo
	if l.Projection != nil {
		proj = &ProjectionInfo{
			ProjectionType: string(l.Projection.ProjectionType),
		}
	}
	var itemCount int64
	if l.ItemCount != nil {
		itemCount = *l.ItemCount
	}
	var sizeBytes int64
	if l.IndexSizeBytes != nil {
		sizeBytes = *l.IndexSizeBytes
	}
	lsis = append(lsis, GSIInfo{
		IndexName:  *l.IndexName,
		KeySchema:  lsiKeys,
		Projection: proj,
		ItemCount:  itemCount,
		SizeBytes:  sizeBytes,
	})
	return lsis
}

// getTableItems handles GET /api/tables/{tableName}/items
func (srv *Server) getTableItems(w http.ResponseWriter, r *http.Request) {
	// Extract table name: URL Path is "/api/tables/{tableName}/items"
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/tables/")
	tableName := strings.TrimSuffix(trimmed, "/items")

	if tableName == "" {
		http.Error(w, "missing table name", http.StatusBadRequest)
		return
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
		}
	}

	if limit > maxTableItemsToDisplay {
		limit = maxTableItemsToDisplay
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	limit32 := int32(limit)
	scanOutput, err := srv.db.Scan(ctx, &dynamodb.ScanInput{
		TableName: &tableName,
		Limit:     &limit32,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	items := []map[string]any{}
	err = attributevalue.UnmarshalListOfMaps(scanOutput.Items, &items)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	scrubbedItems := srv.guardrail.ScrubItems("scan_table", tableName, items)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(scrubbedItems)
}
