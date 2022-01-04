#!/usr/bin/bash

set -ex

until curl -s http://localhost:8091/pools >/dev/null; do
  sleep 5
done

echo "couchbase is up and running, configuring..."

# initialize cluster
echo "Initializing cluster..."
/opt/couchbase/bin/couchbase-cli cluster-init \
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

echo "Creating cb_bucket bucket (dev)..."
/opt/couchbase/bin/couchbase-cli bucket-create \
  --cluster localhost \
  --username Administrator \
  --password password \
  --bucket 'dev' \
  --bucket-type couchbase \
  --bucket-ramsize 512 \
  --wait

echo "Creating scope and collections..."
/opt/couchbase/bin/couchbase-cli collection-manage \
  --cluster localhost:8091 \
  --username Administrator \
  --password password \
  --bucket 'dev' \
  --create-scope nfts

/opt/couchbase/bin/couchbase-cli collection-manage \
  --cluster localhost:8091 \
  --username Administrator \
  --password password \
  --bucket 'dev' \
  --create-collection 'nfts.sales'

echo "pausing for services to come up..."
sleep 15

/opt/couchbase/bin/cbq -u Administrator -p password -s="CREATE PRIMARY INDEX ON \`dev\`.nfts.sales;"
/opt/couchbase/bin/cbq -u Administrator -p password -s="CREATE INDEX adv_publishDetails_saleTime ON \`default\`:\`dev\`.\`nfts\`.\`sales\`(\`publishDetails\`,\`saleTime\`);"