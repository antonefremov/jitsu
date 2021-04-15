package test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/jitsucom/jitsu/server/logging"
	"github.com/testcontainers/testcontainers-go"
	tcWait "github.com/testcontainers/testcontainers-go/wait"
)

const (
	mysqlDefaultPort = "3306/tcp"
	mysqlUser        = "test"
	mysqlPassword    = "test"
	mysqlDatabase    = "test"
	mysqlSchema      = "public"

	envMysqlPortVariable = "MS_TEST_PORT"
)

//MysqlContainer is a Mysql testcontainer
type MysqlContainer struct {
	Container testcontainers.Container
	Context   context.Context
	Host      string
	Port      int
	Database  string
	Schema    string
	Username  string
	Password  string
}

//NewMysqlContainer creates new Mysql test container if MS_TEST_PORT is not defined. Otherwise uses db at defined port. This logic is required
//for running test at CI environment
func NewMysqlContainer(ctx context.Context) (*MysqlContainer, error) {
	if os.Getenv(envMysqlPortVariable) != "" {
		port, err := strconv.Atoi(os.Getenv(envMysqlPortVariable))
		if err != nil {
			return nil, err
		}
		return &MysqlContainer{Context: ctx, Host: "localhost", Port: port,
			Schema: mysqlSchema, Database: mysqlDatabase, Username: mysqlUser, Password: mysqlPassword}, nil
	}
	dbSettings := make(map[string]string, 0)
	dbSettings["MYSQL_USER"] = mysqlUser
	dbSettings["MYSQL_ROOT_PASSWORD"] = mysqlPassword
	dbSettings["MYSQL_DATABASE"] = mysqlDatabase
	dbURL := func(port nat.Port) string {
		return fmt.Sprintf("mysql://%s:%s@localhost:%s/%s?sslmode=disable", mysqlUser, mysqlPassword, port.Port(), mysqlDatabase)
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mysql:latest",
			ExposedPorts: []string{"3306/tcp", "33060/tcp"}, //[]string{mysqlDefaultPort},
			Env:          dbSettings,
			WaitingFor:   tcWait.ForSQL(mysqlDefaultPort, "mysql", dbURL).Timeout(time.Second * 15),
		},
		Started: true,
	})
	if err != nil {
		return nil, err
	}
	host, err := container.Host(ctx)
	if err != nil {
		return nil, err
	}
	port, err := container.MappedPort(ctx, "3306")
	if err != nil {
		return nil, err
	}
	return &MysqlContainer{Container: container, Context: ctx, Host: host, Port: port.Int(),
		Schema: mysqlSchema, Database: mysqlDatabase, Username: mysqlUser, Password: mysqlPassword}, nil
}

//CountRows returns row count in DB table with name = table
//or error if occurred
func (pgc *MysqlContainer) CountRows(table string) (int, error) {
	connectionString := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		pgc.Host, pgc.Port, pgc.Database, pgc.Username, pgc.Password)
	dataSource, err := sql.Open("mysql", connectionString)
	if err != nil {
		return -1, err
	}
	rows, err := dataSource.Query(fmt.Sprintf("SELECT count(*) from %s", table))
	if err != nil {
		errMessage := err.Error()
		if strings.HasPrefix(errMessage, "pq: relation") && strings.HasSuffix(errMessage, "does not exist") {
			return 0, err
		}

		return -1, err
	}
	defer rows.Close()
	rows.Next()
	var count int
	err = rows.Scan(&count)
	return count, err
}

//GetAllSortedRows returns all selected row from table ordered according to orderClause
//or error if occurred
func (pgc *MysqlContainer) GetAllSortedRows(table, orderClause string) ([]map[string]interface{}, error) {
	connectionString := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		pgc.Host, pgc.Port, pgc.Database, pgc.Username, pgc.Password)
	dataSource, err := sql.Open("mysql", connectionString)
	if err != nil {
		return nil, err
	}
	rows, err := dataSource.Query(fmt.Sprintf("SELECT * from %s %s", table, orderClause))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()

	objects := []map[string]interface{}{}
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		// Scan the result into the column pointers...
		if err := rows.Scan(columnPointers...); err != nil {
			return nil, err
		}

		// Create our map, and retrieve the value for each column from the pointers slice,
		// storing it in the map with the name of the column as the key.
		object := make(map[string]interface{})
		for i, colName := range cols {
			val := columnPointers[i].(*interface{})
			object[colName] = *val
		}

		objects = append(objects, object)
	}

	return objects, nil
}

//Close terminates underlying docker container
func (pgc *MysqlContainer) Close() {
	if pgc.Container != nil {
		err := pgc.Container.Terminate(pgc.Context)
		if err != nil {
			logging.Error("Failed to stop container")
		}
	}
}
