#!/bin/bash

set -exm

echo "starting couchbase..."
/entrypoint.sh couchbase-server &

until curl -s http://localhost:8091/pools >/dev/null; do
  sleep 5
done

echo "couchbase is up and running, configuring..."

# check if cluster is already initialized
if ! couchbase-cli server-list -c localhost:8091 -u Administrator -p password >/dev/null; then

  # initialize cluster
  echo "Initializing cluster..."
  couchbase-cli cluster-init \
    --services data,index,query \
    --index-storage-setting default \
    --cluster-ramsize 1024 \
    --cluster-index-ramsize 256 \
    --cluster-analytics-ramsize 0 \
    --cluster-eventing-ramsize 0 \
    --cluster-fts-ramsize 0 \
    --cluster-username Administrator \
    --cluster-password password \
    --cluster-name dockercompose

  echo "Creating cb_bucket bucket (local)..."
  couchbase-cli bucket-create \
    --cluster localhost \
    --username Administrator \
    --password password \
    --bucket 'local' \
    --bucket-type couchbase \
    --bucket-ramsize 512 \
    --wait

  echo "Creating scope and collections..."
  couchbase-cli collection-manage \
    --cluster localhost:8091 \
    --username Administrator \
    --password password \
    --bucket 'local' \
    --create-scope nfts

  couchbase-cli collection-manage \
    --cluster localhost:8091 \
    --username Administrator \
    --password password \
    --bucket 'local' \
    --create-collection 'nfts.sales'

  echo "pausing for services to come up..."
  sleep 15

  cbq -u Administrator -p password -s="CREATE PRIMARY INDEX ON \`local\`.nfts.sales;"
  bq -u Administrator -p password -s="CREATE INDEX adv_publishDetails_saleTime ON \`default\`:\`local\`.\`nfts\`.\`sales\`(\`publishDetails\`,\`saleTime\`);"
fi

fg 1