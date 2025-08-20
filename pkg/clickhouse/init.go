package clickhouse

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	_ "github.com/ClickHouse/clickhouse-go/v2" // For register database driver.
)

const (
	defaultDatabase = "default"
	sqlCreateFolder = "sqlscript/create_table"
	distributeSql   = "sqlscript/distributed-table.tmpl.sql"
	migrateTableSql = "sqlscript/migrate.tmpl.sql"

	templateCreateDb            = "CREATE DATABASE IF NOT EXISTS %s"
	templateCreateDbWithCluster = "CREATE DATABASE IF NOT EXISTS %s ON CLUSTER %s"
)

type ClickHouseInit struct {
	endpoint      string
	database      string
	replication   bool
	cluster       string
	userName      string
	password      string
	createTable   bool
	defaultTTLDay uint
	tableTTLs     map[string]uint
	tableHashKeys map[string]string
	conn          *sql.DB
}

func NewClickHouseInit(
	endpoint string,
	database string,
	replication bool,
	cluster string,
	userName string,
	password string,
	createTable bool,
	defaultTTLDay uint,
	tableTTLs map[string]uint,
	tableHashKeys map[string]string) *ClickHouseInit {
	return &ClickHouseInit{
		endpoint:      endpoint,
		database:      database,
		replication:   replication,
		cluster:       cluster,
		userName:      userName,
		password:      password,
		createTable:   createTable,
		defaultTTLDay: defaultTTLDay,
		tableTTLs:     tableTTLs,
		tableHashKeys: tableHashKeys,
	}
}

func (ch *ClickHouseInit) GetConn() *sql.DB {
	return ch.conn
}

func (ch *ClickHouseInit) Start() (err error) {
	if ch.createTable {
		if err = createDatabase(context.Background(), ch.endpoint, ch.database, ch.cluster, ch.userName, ch.password); err != nil {
			return
		}
	}

	if ch.conn, err = buildDB(ch.endpoint, ch.database, ch.userName, ch.password); err != nil {
		return err
	}

	if ch.createTable {
		err = ch.runInitScripts()
		if err != nil {
			return err
		}
	}
	err = ch.runMigrateScripts()
	if err != nil {
		// Failed to run migrate scripts, maybe no need to migrate/upgrade.
		// This is not an error case, just ignore and print the message.
		log.Println(err)
	}
	return nil
}

func (ch *ClickHouseInit) runInitScripts() error {
	filePaths, err := walkMatch(sqlCreateFolder, "*.tmp.sql")
	if err != nil {
		return fmt.Errorf("could not list sql files: %q", err)
	}
	sort.Strings(filePaths)

	args := tableArgs{
		TTLDay:      ch.defaultTTLDay,
		Replication: ch.replication,
		Cluster:     ch.cluster,
	}
	distargs := distributedTableArgs{
		Cluster:  ch.cluster,
		Database: ch.database,
		Hash:     "rand()",
	}
	sqlStatements := make([]string, 0)
	for _, f := range filePaths {
		_, fileName := filepath.Split(f)
		tmpl, err := template.ParseFiles(filepath.Clean(f))
		if err != nil {
			return err
		}
		tableName := fileName[0 : len(fileName)-8]
		if ttlDay, found := ch.tableTTLs[tableName]; found {
			args.TTLDay = ttlDay
		} else {
			args.TTLDay = ch.defaultTTLDay
		}
		var rendered bytes.Buffer
		if err := tmpl.Execute(&rendered, args); err != nil {
			return err
		}
		sqlStatements = append(sqlStatements, rendered.String())

		if ch.cluster != "" {
			disttmpl, err := template.ParseFiles(distributeSql)
			if err != nil {
				return err
			}
			distargs.Table = tableName
			if hashKey, exist := ch.tableHashKeys[tableName]; exist {
				distargs.Hash = hashKey
			} else {
				distargs.Hash = "rand()"
			}
			var distRendered bytes.Buffer
			if err := disttmpl.Execute(&distRendered, distargs); err != nil {
				return err
			}
			sqlStatements = append(sqlStatements, distRendered.String())
		}
	}

	for _, sqlStatement := range sqlStatements {
		_, err := ch.conn.ExecContext(context.Background(), sqlStatement)
		if err != nil {
			return fmt.Errorf("could not run sql %q: %q", sqlStatement, err)
		}
	}

	return nil
}

// runMigrateScripts Run scripts to upgrade the table schema
func (ch *ClickHouseInit) runMigrateScripts() error {
	fs, err := os.Stat(migrateTableSql)
	if os.IsNotExist(err) {
		return nil
	}
	if fs.Size() < 100 {
		return nil
	}

	args := distributedTableArgs{
		Cluster:  ch.cluster,
		Database: ch.database,
	}
	migrateTemplate, err := template.ParseFiles(migrateTableSql)
	if err != nil {
		return err
	}
	var rendered bytes.Buffer
	if err := migrateTemplate.Execute(&rendered, args); err != nil {
		return err
	}
	sqlStatements := rendered.String()
	log.Println("Try to run migrate scripts: " + sqlStatements)
	err = executeMultiStatements(ch.conn, sqlStatements)
	if err != nil {
		return fmt.Errorf("failed to run migrate sql, maybe no need to migrate/upgrade %q: %q", sqlStatements, err)
	}
	return nil
}

func executeMultiStatements(conn *sql.DB, sqlStatements string) error {
	statements := strings.Split(sqlStatements, ";")
	for _, stmt := range statements {
		trimmedStmt := strings.TrimSpace(stmt)
		if trimmedStmt != "" {
			_, err := conn.ExecContext(context.Background(), trimmedStmt)
			if err != nil {
				if strings.Contains(trimmedStmt, "REMOVE TTL") {
					// Ignore Remove TTL failed.
					continue
				}
				return err
			}
		}
	}

	return nil
}

func createDatabase(ctx context.Context, endpoint string, database string, cluster string, userName string, password string) error {
	// use default database to create new database
	if database == defaultDatabase {
		return nil
	}

	db, err := buildDB(endpoint, defaultDatabase, userName, password)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()
	var query string
	if cluster == "" {
		query = fmt.Sprintf(templateCreateDb, database)
	} else {
		query = fmt.Sprintf(templateCreateDbWithCluster, database, cluster)
	}

	_, err = db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("create database:%w", err)
	}
	return nil
}

func buildDSN(endpoint string, database string, userName string, password string) (string, error) {
	dsnURL, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("%w: %s", errConfigInvalidEndpoint, err.Error())
	}

	queryParams := dsnURL.Query()
	// Enable TLS if scheme is https. This flag is necessary to support https connections.
	if dsnURL.Scheme == "https" {
		queryParams.Set("secure", "true")
	}

	// Use default database if not specified in any other place.
	if database == "" {
		dsnURL.Path = defaultDatabase
	} else {
		dsnURL.Path = database
	}
	// Override username and password if specified in config.
	if userName != "" {
		dsnURL.User = url.UserPassword(userName, password)
	}
	dsnURL.RawQuery = queryParams.Encode()

	return dsnURL.String(), nil
}

func buildDB(endpoint string, database string, userName string, password string) (*sql.DB, error) {
	dsn, err := buildDSN(endpoint, database, userName, password)
	if err != nil {
		return nil, err
	}

	// ClickHouse sql driver will read clickhouse settings from the DSN string.
	// It also ensures defaults.
	// See https://github.com/ClickHouse/clickhouse-go/blob/08b27884b899f587eb5c509769cd2bdf74a9e2a1/clickhouse_std.go#L189
	conn, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

func walkMatch(root, pattern string) ([]string, error) {
	var matches []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if matched, err := filepath.Match(pattern, filepath.Base(path)); err != nil {
			return err
		} else if matched {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

type tableArgs struct {
	TTLDay      uint
	Replication bool
	Cluster     string
}

type distributedTableArgs struct {
	Cluster  string
	Database string
	Table    string
	Hash     string
}
