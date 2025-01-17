---
sort: 2
---

import {Hint} from "../../../components/documentationComponents";

# Deploying with Docker

**Jitsu** provides a Docker image to simplify deployment on your IaaS or hardware of choice. We build two images:

* `jitsucom/server:latest` — contains the latest stable production release. This image is built from [master](https://github.com/jitsucom/jitsu/tree/master) branch
* `jitsucom/server:beta` — contains the latest beta version built from [beta](https://github.com/jitsucom/jitsu/tree/beta) branch

We recommend using beta for experiments. It's stable and well tested. It usually takes 2-4 months for the feature to graduate from beta to stable. This guide uses beta build. Just replace is with latest if you want to run a stable version

### Getting started with Docker

* Pull the image from Docker Hub with: `docker pull jitsucom/server:beta`
* Create an `<data_dir>`. It will be used as Docker mount directory for keeping Jitsu config and logs.
* Create [your config file](/docs/configuration/) and save it in `<data_dir>/config/eventnative.yaml`.

<Hint>
    Make sure &lt;data_dir&gt; directory have right permissions or just run <code inline="true">chmod -R 777 &lt;data_dir&gt;</code>
</Hint>

* Run the Docker image and mount your config file with the following command:

```javascript
docker run -p <local_port>:8001 \
  -v /<data_dir>/:/home/eventnative/data/ \
  jitsucom/server:beta
```

Please, refer `<data_dir>` by its absolute path. Use `$PWD` macro if necessary. Example:

```javascript
docker run --name jitsu-test -p 8000:8001 \
  -v $PWD/data/:/home/eventnative/data/ \
  jitsucom/server:beta
```

Also, **Jitsu** supports passing config via `CONFIG_LOCATION` environment variable. The configuration might be one of the [described formats](/docs/deployment/configuration-source). For example, docker run with externalized [HTTP configuration source](/docs/deployment/configuration-source#http-source):

```javascript
docker run --name jitsu-test -p 8000:8001 \n
-e CONFIG_LOCATION='https://username:password@config-server.com?env=dev' \
  jitsucom/server:beta
```


Once you see Started banner in logs, it **Jitsu** is running.