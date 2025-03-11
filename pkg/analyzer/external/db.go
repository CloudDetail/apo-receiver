package external

import (
	"fmt"
	"log"

	apmclient "github.com/CloudDetail/apo-module/apm/client/v1"
	"github.com/CloudDetail/apo-module/apm/model/v1"
)

type dbParser struct {
}

func newDbParser() *dbParser {
	return &dbParser{}
}

func (db *dbParser) Parse(span *model.OtelSpan) *External {
	if span.Kind != model.SpanKindClient {
		return nil
	}
	dbSystem, dbFound := span.Attributes[model.AttributeDBSystem]
	if !dbFound {
		return nil
	}
	name := ""
	if dbSystem == "redis" || dbSystem == "memcached" || dbSystem == "aerospike" {
		name = span.Name
	}
	dbStatement := span.Attributes[model.AttributeDBStatement]
	operationName := span.Attributes[model.AttributeDBOperation]
	if name == "" {
		dbName := span.Attributes[model.AttributeDBName]
		tableName := span.Attributes[model.AttributeDBSQLTable]
		if operationName != "" {
			if dbName != "" {
				// SELECT <db>.<table>
				name = fmt.Sprintf("%s %s.%s", operationName, dbName, tableName)
			} else {
				// SELECT <table>
				name = fmt.Sprintf("%s %s", operationName, tableName)
			}
		} else if dbStatement != "" {
			if operationName, tableName = apmclient.SQLParseOperationAndTableNEW(dbStatement); operationName != "" {
				if dbName != "" {
					name = fmt.Sprintf("%s %s.%s", operationName, dbName, tableName)
				} else {
					name = fmt.Sprintf("%s %s", operationName, tableName)
				}
			} else {
				log.Printf("[x Parse Sql] %s", dbStatement)
			}
		}
	}
	if name == "" {
		name = span.Name
	}
	dbDetail := dbStatement
	if dbStatement == "" {
		dbDetail = operationName
	}

	return newExternal(span).
		WithGroup(GroupDb).
		WithType(dbSystem).
		WithName(name).
		WithPeer(span.GetPeer("")).
		WithDetail(dbDetail)
}
