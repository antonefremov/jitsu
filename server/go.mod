module github.com/jitsucom/jitsu/server

go 1.14

require (
	bou.ke/monkey v1.0.2
	cloud.google.com/go/bigquery v1.4.0
	cloud.google.com/go/firestore v1.1.1
	cloud.google.com/go/storage v1.6.0
	firebase.google.com/go/v4 v4.1.0
	github.com/aws/aws-sdk-go v1.34.0
	github.com/docker/go-connections v0.4.0
	github.com/gin-gonic/gin v1.6.3
	github.com/go-sql-driver/mysql v1.5.0
	github.com/gomodule/redigo v1.8.2
	github.com/google/go-github/v32 v32.1.0
	github.com/google/martian v2.1.0+incompatible
	github.com/google/uuid v1.2.0
	github.com/gookit/color v1.3.1
	github.com/hashicorp/go-multierror v1.1.0
	github.com/huandu/facebook/v2 v2.5.3
	github.com/joncrlsn/dque v0.0.0-20200702023911-3e80e3146ce5
	github.com/lib/pq v1.8.0
	github.com/mailru/easyjson v0.7.7
	github.com/mailru/go-clickhouse v1.3.0
	github.com/mitchellh/hashstructure/v2 v2.0.1
	github.com/oschwald/geoip2-golang v1.4.0
	github.com/panjf2000/ants/v2 v2.4.3
	github.com/prometheus/client_golang v1.7.1
	github.com/robfig/cron/v3 v3.0.1
	github.com/satori/go.uuid v1.2.0
	github.com/snowflakedb/gosnowflake v1.3.8
	github.com/spf13/cast v1.3.0
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.7.0
	github.com/testcontainers/testcontainers-go v0.10.0
	github.com/ua-parser/uap-go v0.0.0-20200325213135-e1c09f13e2fe
	go.etcd.io/etcd/client/v3 v3.5.0-alpha.0
	go.opencensus.io v0.22.4 // indirect
	go.uber.org/atomic v1.6.0
	google.golang.org/api v0.20.0
	google.golang.org/appengine v1.6.6 // indirect
	google.golang.org/grpc v1.36.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	gotest.tools v2.2.0+incompatible
	honnef.co/go/tools v0.0.1-2020.1.4 // indirect
)

replace (
	github.com/coreos/etcd => go.etcd.io/etcd/v3 v3.5.0-alpha.0
	google.golang.org/grpc v1.36.0 => google.golang.org/grpc v1.29.1
)
