package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jitsucom/jitsu/server/logging"
	"github.com/jitsucom/jitsu/server/typing"
)

const (
	tableNamesQueryMysql  = `SELECT table_name FROM information_schema.tables WHERE table_schema=$1`
	tableSchemaQueryMysql = `SELECT 
 							pg_attribute.attname AS name,
    						pg_catalog.format_type(pg_attribute.atttypid,pg_attribute.atttypmod) AS column_type
						FROM pg_attribute
         					JOIN pg_class ON pg_class.oid = pg_attribute.attrelid
         					LEFT JOIN pg_attrdef pg_attrdef ON pg_attrdef.adrelid = pg_class.oid AND pg_attrdef.adnum = pg_attribute.attnum
         					LEFT JOIN pg_namespace ON pg_namespace.oid = pg_class.relnamespace
         					LEFT JOIN pg_constraint ON pg_constraint.conrelid = pg_class.oid AND pg_attribute.attnum = ANY (pg_constraint.conkey)
						WHERE pg_class.relkind = 'r'::char
  							AND  pg_namespace.nspname = $1
  							AND pg_class.relname = $2
  							AND pg_attribute.attnum > 0`
	primaryKeyFieldsQueryMysql = `SELECT
							pg_attribute.attname
						FROM pg_index, pg_class, pg_attribute, pg_namespace
						WHERE
								pg_class.oid = $1::regclass AND
								indrelid = pg_class.oid AND
								nspname = $2 AND
								pg_class.relnamespace = pg_namespace.oid AND
								pg_attribute.attrelid = pg_class.oid AND
								pg_attribute.attnum = any(pg_index.indkey)
					  	AND indisprimary`
	createDbSchemaIfNotExistsTemplateMysql = `CREATE SCHEMA IF NOT EXISTS "%s"`
	addColumnTemplateMysql                 = `ALTER TABLE "%s"."%s" ADD COLUMN %s`
	dropPrimaryKeyTemplateMysql            = "ALTER TABLE %s.%s DROP CONSTRAINT %s"
	alterPrimaryKeyTemplateMysql           = `ALTER TABLE "%s"."%s" ADD CONSTRAINT %s PRIMARY KEY (%s)`
	createTableTemplateMysql               = `CREATE TABLE "%s"."%s" (%s)`
	insertTemplateMysql                    = `INSERT INTO "%s"."%s" (%s) VALUES %s`
	mergeTemplateMysql                     = `INSERT INTO "%s"."%s"(%s) VALUES %s ON CONFLICT ON CONSTRAINT %s DO UPDATE set %s;`
	deleteQueryTemplateMysql               = `DELETE FROM "%s"."%s" WHERE %s`

	copyColumnTemplateMysql   = `UPDATE "%s"."%s" SET %s = %s`
	dropColumnTemplateMysql   = `ALTER TABLE "%s"."%s" DROP COLUMN %s`
	renameColumnTemplateMysql = `ALTER TABLE "%s"."%s" RENAME COLUMN %s TO %s`

	mysqlValuesLimit                        = 65535 // this is a limitation of parameters one can pass as query values. If more parameters are passed, error is returned
)

var (
	SchemaToMysql = map[typing.DataType]string{
		typing.STRING:    "text",
		typing.INT64:     "bigint",
		typing.FLOAT64:   "float(38,18)",
		typing.TIMESTAMP: "timestamp",
		typing.BOOL:      "boolean",
		typing.UNKNOWN:   "text",
	}
)

//DataSourceConfigMysql dto for deserialized datasource config (e.g. in Mysql or AwsRedshift destination)
type DataSourceConfigMysql struct {
	Host       string            `mapstructure:"host" json:"host,omitempty" yaml:"host,omitempty"`
	Port       json.Number       `mapstructure:"port" json:"port,omitempty" yaml:"port,omitempty"`
	Db         string            `mapstructure:"db" json:"db,omitempty" yaml:"db,omitempty"`
	Schema     string            `mapstructure:"schema" json:"schema,omitempty" yaml:"schema,omitempty"`
	Username   string            `mapstructure:"username" json:"username,omitempty" yaml:"username,omitempty"`
	Password   string            `mapstructure:"password" json:"password,omitempty" yaml:"password,omitempty"`
	Parameters map[string]string `mapstructure:"parameters" json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

//Validate required fields in DataSourceConfigMysql
func (dsc *DataSourceConfigMysql) Validate() error {
	if dsc == nil {
		return errors.New("Datasource config is required")
	}
	if dsc.Host == "" {
		return errors.New("Datasource host is required parameter")
	}
	if dsc.Db == "" {
		return errors.New("Datasource db is required parameter")
	}
	if dsc.Username == "" {
		return errors.New("Datasource username is required parameter")
	}

	if dsc.Parameters == nil {
		dsc.Parameters = map[string]string{}
	}
	return nil
}

//MySql is adapter for creating,patching (schema or table), inserting data to MySQL
type Mysql struct {
	ctx         context.Context
	config      *DataSourceConfigMysql
	dataSource  *sql.DB
	queryLogger *logging.QueryLogger

	mappingTypeCasts map[string]string
}

//NewMysqlUnderRedshift return configured Mysql adapter instance without mapping old types
func NewMysqlUnderRedshift(ctx context.Context, config *DataSourceConfigMysql, queryLogger *logging.QueryLogger, mappingTypeCasts map[string]string) (*Mysql, error) {
	connectionString := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s ",
		config.Host, config.Port.String(), config.Db, config.Username, config.Password)
	//concat provided connection parameters
	for k, v := range config.Parameters {
		connectionString += k + "=" + v + " "
	}
	dataSource, err := sql.Open("mysql", connectionString)
	if err != nil {
		return nil, err
	}

	if err := dataSource.Ping(); err != nil {
		dataSource.Close()
		return nil, err
	}

	//set default value
	dataSource.SetConnMaxLifetime(10 * time.Minute)

	return &Mysql{ctx: ctx, config: config, dataSource: dataSource, queryLogger: queryLogger, mappingTypeCasts: mappingTypeCasts}, nil
}

//NewMysql return configured Mysql adapter instance
func NewMysql(ctx context.Context, config *DataSourceConfigMysql, queryLogger *logging.QueryLogger, mappingTypeCasts map[string]string) (*Mysql, error) {
	connectionString := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s ",
		config.Host, config.Port.String(), config.Db, config.Username, config.Password)
	//concat provided connection parameters
	for k, v := range config.Parameters {
		connectionString += k + "=" + v + " "
	}
	dataSource, err := sql.Open("mysql", connectionString)
	if err != nil {
		return nil, err
	}

	if err := dataSource.Ping(); err != nil {
		dataSource.Close()
		return nil, err
	}

	//set default value
	dataSource.SetConnMaxLifetime(10 * time.Minute)

	return &Mysql{ctx: ctx, config: config, dataSource: dataSource, queryLogger: queryLogger, mappingTypeCasts: reformatMappings(mappingTypeCasts, SchemaToMysql)}, nil
}

func (Mysql) Name() string {
	return "Mysql"
}

//OpenTx open underline sql transaction and return wrapped instance
func (p *Mysql) OpenTx() (*Transaction, error) {
	tx, err := p.dataSource.BeginTx(p.ctx, nil)
	if err != nil {
		return nil, err
	}

	return &Transaction{tx: tx, dbType: p.Name()}, nil
}

//CreateDbSchema create database schema instance if doesn't exist
func (p *Mysql) CreateDbSchema(dbSchemaName string) error {
	wrappedTx, err := p.OpenTx()
	if err != nil {
		return err
	}

	return createDbSchemaInTransactionMysql(p.ctx, wrappedTx, createDbSchemaIfNotExistsTemplate, dbSchemaName, p.queryLogger)
}

//CreateTable create database table with name,columns provided in Table representation
func (p *Mysql) CreateTable(table *Table) error {
	wrappedTx, err := p.OpenTx()
	if err != nil {
		return err
	}

	return p.createTableInTransaction(wrappedTx, table)
}

//PatchTableSchema add new columns(from provided Table) to existing table
func (p *Mysql) PatchTableSchema(patchTable *Table) error {
	wrappedTx, err := p.OpenTx()
	if err != nil {
		return err
	}

	return p.patchTableSchemaInTransaction(wrappedTx, patchTable)
}

//GetTableSchema return table (name,columns with name and types) representation wrapped in Table struct
func (p *Mysql) GetTableSchema(tableName string) (*Table, error) {
	table, err := p.getTable(tableName)
	if err != nil {
		return nil, err
	}

	//don't select primary keys of non-existent table
	if len(table.Columns) == 0 {
		return table, nil
	}

	pkFields, err := p.getPrimaryKeys(tableName)
	if err != nil {
		return nil, err
	}

	table.PKFields = pkFields
	return table, nil
}

func (p *Mysql) getTable(tableName string) (*Table, error) {
	table := &Table{Name: tableName, Columns: map[string]Column{}, PKFields: map[string]bool{}}
	rows, err := p.dataSource.QueryContext(p.ctx, tableSchemaQuery, p.config.Schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("Error querying table [%s] schema: %v", tableName, err)
	}

	defer rows.Close()
	for rows.Next() {
		var columnName, columnMysqlType string
		if err := rows.Scan(&columnName, &columnMysqlType); err != nil {
			return nil, fmt.Errorf("Error scanning result: %v", err)
		}
		if columnMysqlType == "-" {
			//skip dropped mysql field
			continue
		}

		table.Columns[columnName] = Column{SQLType: columnMysqlType}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Last rows.Err: %v", err)
	}

	return table, nil
}

//create table columns and pk key
//override input table sql type with configured cast type
//make fields from Table PkFields - 'not null'
func (p *Mysql) createTableInTransaction(wrappedTx *Transaction, table *Table) error {
	var columnsDDL []string
	pkFields := table.GetPKFieldsMap()
	for columnName, column := range table.Columns {
		columnsDDL = append(columnsDDL, p.columnDDL(columnName, column, pkFields))
	}

	//sorting columns asc
	sort.Strings(columnsDDL)
	query := fmt.Sprintf(createTableTemplate, p.config.Schema, table.Name, strings.Join(columnsDDL, ","))
	p.queryLogger.LogDDL(query)

	_, err := wrappedTx.tx.ExecContext(p.ctx, query)

	if err != nil {
		wrappedTx.Rollback()
		return fmt.Errorf("Error creating [%s] table: %v", table.Name, err)
	}

	err = p.createPrimaryKeyInTransaction(wrappedTx, table)
	if err != nil {
		wrappedTx.Rollback()
		return err
	}

	return wrappedTx.tx.Commit()
}

//alter table with columns (if not empty)
//recreate primary key (if not empty) or delete primary key if Table.DeletePkFields is true
func (p *Mysql) patchTableSchemaInTransaction(wrappedTx *Transaction, patchTable *Table) error {
	pkFields := patchTable.GetPKFieldsMap()
	//patch columns
	for columnName, column := range patchTable.Columns {
		columnDDL := p.columnDDL(columnName, column, pkFields)
		query := fmt.Sprintf(addColumnTemplate, p.config.Schema, patchTable.Name, columnDDL)
		p.queryLogger.LogDDL(query)

		_, err := wrappedTx.tx.ExecContext(p.ctx, query)
		if err != nil {
			wrappedTx.Rollback()
			return fmt.Errorf("Error patching %s table with [%s] DDL: %v", patchTable.Name, columnDDL, err)
		}
	}

	//patch primary keys - delete old
	if patchTable.DeletePkFields {
		err := p.deletePrimaryKeyInTransaction(wrappedTx, patchTable)
		if err != nil {
			wrappedTx.Rollback()
			return err
		}
	}

	//patch primary keys - create new
	if len(patchTable.PKFields) > 0 {
		err := p.createPrimaryKeyInTransaction(wrappedTx, patchTable)
		if err != nil {
			wrappedTx.Rollback()
			return err
		}
	}

	return wrappedTx.DirectCommit()
}

//createPrimaryKeyInTransaction create primary key constraint
//re-create fields as not null (if unable to create constraint)
func (p *Mysql) createPrimaryKeyInTransaction(wrappedTx *Transaction, table *Table) error {
	if len(table.PKFields) == 0 {
		return nil
	}

	query := fmt.Sprintf(alterPrimaryKeyTemplate,
		p.config.Schema, table.Name, buildConstraintName(p.config.Schema, table.Name), strings.Join(table.GetPKFields(), ","))
	p.queryLogger.LogDDL(query)

	_, err := wrappedTx.tx.ExecContext(p.ctx, query)
	if err != nil {
		return fmt.Errorf("Error setting primary key [%s] %s table: %v", strings.Join(table.GetPKFields(), ","), table.Name, err)
	}

	return nil
}

//delete primary key
func (p *Mysql) deletePrimaryKeyInTransaction(wrappedTx *Transaction, table *Table) error {
	query := fmt.Sprintf(dropPrimaryKeyTemplate, p.config.Schema, table.Name, buildConstraintName(p.config.Schema, table.Name))
	p.queryLogger.LogDDL(query)
	_, err := wrappedTx.tx.ExecContext(p.ctx, query)
	if err != nil {
		return fmt.Errorf("Failed to drop primary key constraint for table %s.%s: %v", p.config.Schema, table.Name, err)
	}

	return nil
}

//Insert provided object in mysql with typecasts
func (p *Mysql) Insert(table *Table, valuesMap map[string]interface{}) error {
	header, placeholders, values := p.buildQueryPayload(valuesMap)
	query := p.insertQuery(table.GetPKFields(), table.Name, header, "("+placeholders+")")
	p.queryLogger.LogQueryWithValues(query, values)

	_, err := p.dataSource.ExecContext(p.ctx, query, values...)
	if err != nil {
		return fmt.Errorf("Error inserting in %s table with statement: %s values: %v: %v", table.Name, query, values, err)
	}

	return nil
}

func (p *Mysql) BulkUpdate(table *Table, objects []map[string]interface{}, deleteConditions *DeleteConditions) error {
	wrappedTx, err := p.OpenTx()
	if err != nil {
		return err
	}

	if !deleteConditions.IsEmpty() {
		err := p.deleteInTransaction(wrappedTx, table, deleteConditions)
		if err != nil {
			wrappedTx.Rollback()
			return err
		}
	}

	if err := p.bulkStoreInTransaction(wrappedTx, table, objects); err != nil {
		wrappedTx.Rollback()
		return err
	}
	return wrappedTx.DirectCommit()
}

func (p *Mysql) deleteInTransaction(wrappedTx *Transaction, table *Table, deleteConditions *DeleteConditions) error {
	deleteCondition, values := p.toDeleteQuery(deleteConditions)
	query := fmt.Sprintf(deleteQueryTemplate, p.config.Schema, table.Name, deleteCondition)
	p.queryLogger.LogQueryWithValues(query, values)
	deleteStmt, err := wrappedTx.tx.PrepareContext(p.ctx, query)
	if err != nil {
		return fmt.Errorf("Error preparing delete table %s statement: %v", table.Name, err)
	}
	_, err = deleteStmt.ExecContext(p.ctx, values...)
	if err != nil {
		return fmt.Errorf("Error deleting using query: %s:, error: %v", query, err)
	}
	return nil
}

func (p *Mysql) toDeleteQuery(conditions *DeleteConditions) (string, []interface{}) {
	var queryConditions []string
	var values []interface{}
	for i, condition := range conditions.Conditions {
		queryConditions = append(queryConditions, condition.Field+" "+condition.Clause+" $"+strconv.Itoa(i+1)+p.castClause(condition.Field))
		values = append(values, condition.Value)
	}
	return strings.Join(queryConditions, conditions.JoinCondition), values
}

func (p *Mysql) castClause(field string) string {
	castClause := ""
	castType, ok := p.mappingTypeCasts[field]
	if ok {
		castClause = "::" + castType
	}
	return castClause
}

//BulkInsert insert objects into table in one transaction
func (p *Mysql) BulkInsert(table *Table, objects []map[string]interface{}) error {
	wrappedTx, err := p.OpenTx()
	if err != nil {
		return err
	}
	if err = p.bulkStoreInTransaction(wrappedTx, table, objects); err != nil {
		wrappedTx.Rollback()
		return err
	}

	return wrappedTx.DirectCommit()
}

func (p *Mysql) bulkStoreInTransaction(wrappedTx *Transaction, table *Table, objects []map[string]interface{}) error {
	if len(table.PKFields) == 0 {
		return p.bulkInsertInTransaction(wrappedTx, table, objects)
	}

	return p.bulkMergeInTransaction(wrappedTx, table, objects)
}

//Must be used when table has no primary keys. Inserts data in batches to improve performance.
//Prefer to use bulkStoreInTransaction instead of calling this method directly
func (p *Mysql) bulkInsertInTransaction(wrappedTx *Transaction, table *Table, objects []map[string]interface{}) error {
	var placeholdersBuilder strings.Builder
	var header []string
	for name := range table.Columns {
		header = append(header, name)
	}
	maxValues := len(objects) * len(table.Columns)
	if maxValues > mysqlValuesLimit {
		maxValues = mysqlValuesLimit
	}
	valueArgs := make([]interface{}, 0, maxValues)
	placeholdersCounter := 1
	for _, row := range objects {
		// if number of values exceeds limit, we have to execute insert query on processed rows
		if len(valueArgs)+len(header) > mysqlValuesLimit {
			err := p.executeInsert(wrappedTx, table, header, placeholdersBuilder, valueArgs)
			if err != nil {
				return err
			}
			placeholdersBuilder.Reset()
			placeholdersCounter = 1
			valueArgs = make([]interface{}, 0, maxValues)
		}
		_, err := placeholdersBuilder.WriteString("(")
		if err != nil {
			return fmt.Errorf(placeholdersStringBuildErrTemplate, err)
		}
		for i, column := range header {
			value, _ := row[column]
			valueArgs = append(valueArgs, value)
			castClause := ""
			castType, ok := p.mappingTypeCasts[column]
			if ok {
				castClause = "::" + castType
			}
			_, err = placeholdersBuilder.WriteString("$" + strconv.Itoa(placeholdersCounter) + castClause)
			if err != nil {
				return fmt.Errorf(placeholdersStringBuildErrTemplate, err)
			}

			if i < len(header)-1 {
				_, err = placeholdersBuilder.WriteString(",")
				if err != nil {
					return fmt.Errorf(placeholdersStringBuildErrTemplate, err)
				}
			}
			placeholdersCounter++
		}
		_, err = placeholdersBuilder.WriteString("),")
		if err != nil {
			return fmt.Errorf(placeholdersStringBuildErrTemplate, err)
		}
	}
	if len(valueArgs) > 0 {
		err := p.executeInsert(wrappedTx, table, header, placeholdersBuilder, valueArgs)
		if err != nil {
			return err
		}
	}
	return nil
}

//Must be used only if table has primary key fields. Slower than bulkInsert as each query executed separately.
//Prefer to use bulkStoreInTransaction instead of calling this method directly
func (p *Mysql) bulkMergeInTransaction(wrappedTx *Transaction, table *Table, objects []map[string]interface{}) error {
	var placeholders string
	var header []string
	i := 1
	for name := range table.Columns {
		header = append(header, name)

		//$1::type, $2::type, $3, etc
		castClause := ""
		castType, ok := p.mappingTypeCasts[name]
		if ok {
			castClause = "::" + castType
		}
		placeholders += "$" + strconv.Itoa(i) + castClause + ","

		i++
	}
	placeholders = "(" + removeLastComma(placeholders) + ")"

	headerClause := strings.Join(header, ",")
	query := fmt.Sprintf(mergeTemplate, p.config.Schema, table.Name, headerClause, placeholders, buildConstraintName(p.config.Schema, table.Name), updateSectionMysql(headerClause))
	mergeStmt, err := wrappedTx.tx.PrepareContext(p.ctx, query)
	if err != nil {
		return fmt.Errorf("Error preparing bulk insert statement [%s] table %s statement: %v", query, table.Name, err)
	}

	for _, row := range objects {
		var values []interface{}
		for _, column := range header {
			value, _ := row[column]
			values = append(values, value)
		}
		p.queryLogger.LogQueryWithValues(query, values)
		_, err = mergeStmt.ExecContext(p.ctx, values...)
		if err != nil {
			return fmt.Errorf("Error bulk inserting in %s table with statement: %s values: %v: %v", table.Name, query, values, err)
		}
	}

	return nil
}

func (p *Mysql) executeInsert(wrappedTx *Transaction, table *Table, header []string, placeholdersBuilder strings.Builder, valueArgs []interface{}) error {
	query := p.insertQuery(table.GetPKFields(), table.Name, strings.Join(header, ","), removeLastComma(placeholdersBuilder.String()))
	_, err := wrappedTx.tx.Exec(query, valueArgs...)
	return err
}

//get insert statement or merge on conflict statement
func (p *Mysql) insertQuery(pkFields []string, tableName string, header string, placeholders string) string {
	if len(pkFields) == 0 {
		return fmt.Sprintf(insertTemplate, p.config.Schema, tableName, header, placeholders)
	} else {
		return fmt.Sprintf(mergeTemplate, p.config.Schema, tableName, header, placeholders, buildConstraintName(p.config.Schema, tableName), updateSectionMysql(header))
	}
}

//TablesList return slice of mysql table names
func (p *Mysql) TablesList() ([]string, error) {
	var tableNames []string
	rows, err := p.dataSource.QueryContext(p.ctx, tableNamesQuery, p.config.Schema)
	if err != nil {
		return tableNames, fmt.Errorf("Error querying tables names: %v", err)
	}

	defer rows.Close()
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return tableNames, fmt.Errorf("Error scanning table name: %v", err)
		}
		tableNames = append(tableNames, tableName)
	}
	if err := rows.Err(); err != nil {
		return tableNames, fmt.Errorf("Last rows.Err: %v", err)
	}

	return tableNames, nil
}

//columnDDL return column DDL (column name, mapped sql type and 'not null' if pk field)
func (p *Mysql) columnDDL(name string, column Column, pkFields map[string]bool) string {
	var notNullClause string
	sqlType := column.SQLType
	//casted
	if castedSQLType, ok := p.mappingTypeCasts[name]; ok {
		sqlType = castedSQLType
	}

	//not null
	if _, ok := pkFields[name]; ok {
		notNullClause = "not null " + p.getDefaultValueStatement(sqlType)
	}

	return fmt.Sprintf(`%s %s %s`, name, sqlType, notNullClause)
}

//return default value statement for creating column
func (p *Mysql) getDefaultValueStatement(sqlType string) string {
	//get default value based on type
	if strings.Contains(sqlType, "var") || strings.Contains(sqlType, "text") {
		return "default ''"
	}

	return "default 0"
}

//Close underlying sql.DB
func (p *Mysql) Close() error {
	return p.dataSource.Close()
}

func updateSectionMysql(header string) string {
	split := strings.Split(header, ",")
	var result string
	for i, columnName := range split {
		result = strings.TrimSpace(result) + columnName + "=$" + strconv.Itoa(i+1) + ","
	}
	return removeLastComma(result)
}

//create database and commit transaction
func createDbSchemaInTransactionMysql(ctx context.Context, wrappedTx *Transaction, statementTemplate,
	dbSchemaName string, queryLogger *logging.QueryLogger) error {
	query := fmt.Sprintf(statementTemplate, dbSchemaName)
	queryLogger.LogDDL(query)
	createStmt, err := wrappedTx.tx.PrepareContext(ctx, query)
	if err != nil {
		wrappedTx.Rollback()
		return fmt.Errorf("Error preparing create db schema %s statement: %v", dbSchemaName, err)
	}

	_, err = createStmt.ExecContext(ctx)

	if err != nil {
		wrappedTx.Rollback()
		return fmt.Errorf("Error creating [%s] db schema: %v", dbSchemaName, err)
	}

	return wrappedTx.tx.Commit()
}

func (p *Mysql) getPrimaryKeys(tableName string) (map[string]bool, error) {
	primaryKeys := map[string]bool{}
	pkFieldsRows, err := p.dataSource.QueryContext(p.ctx, primaryKeyFieldsQuery, p.config.Schema+"."+tableName, p.config.Schema)
	if err != nil {
		return nil, fmt.Errorf("Error querying primary keys for [%s.%s] table: %v", p.config.Schema, tableName, err)
	}

	defer pkFieldsRows.Close()
	var pkFields []string
	for pkFieldsRows.Next() {
		var fieldName string
		if err := pkFieldsRows.Scan(&fieldName); err != nil {
			return nil, fmt.Errorf("error scanning primary key result: %v", err)
		}
		pkFields = append(pkFields, fieldName)
	}
	if err := pkFieldsRows.Err(); err != nil {
		return nil, fmt.Errorf("pk last rows.Err: %v", err)
	}
	for _, field := range pkFields {
		primaryKeys[field] = true
	}

	return primaryKeys, nil
}

func (p *Mysql) buildQueryPayload(valuesMap map[string]interface{}) (string, string, []interface{}) {
	header := make([]string, len(valuesMap), len(valuesMap))
	placeholders := make([]string, len(valuesMap), len(valuesMap))
	values := make([]interface{}, len(valuesMap), len(valuesMap))
	i := 0
	for name, value := range valuesMap {
		header[i] = name
		//$1::type, $2::type, $3, etc ($0 - wrong)
		placeholders[i] = fmt.Sprintf("$%d%s", i+1, p.castClause(name))
		values[i] = value
		i++
	}

	return strings.Join(header, ", "), strings.Join(placeholders, ", "), values
}
