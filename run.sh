#!/bin/bash

# INPUT Parameters
#
# $1 - S3 bucket to use
# $2 - AWS Account ID

#
# Helper function that runs whatever command is passed to it and exits if command does not return zero status
# This could be extended to clean up if required
#
function run {
    "$@"
    local status=$?
    if [ $status -ne 0 ]; then
        echo "$1 errored with $status" >&2
        exit $status
    fi
    return $status
}

# INSTALL PRE-REQS
sudo mkdir /opt/drill
sudo mkdir /opt/drill/log
sudo chmod 777 /opt/drill/log
sudo curl -s "http://download.nextag.com/apache/drill/drill-1.9.0/apache-drill-1.9.0.tar.gz" | sudo tar xz --strip=1 -C /opt/drill
sudo yum install -y java-1.8.0-openjdk
sudo yum install -y python35
sudo yum install -y aws-cli
sudo yum install -y unzip

# Add Drill to PATH
export PATH=/opt/drill/bin:$PATH

# Start Athena Proxy
PORT=10000 java -cp ./athenaproxy/athenaproxy.jar com.getredash.awsathena_proxy.API . &

DBRFILES3="s3://${1}/${2}-aws-billing-detailed-line-items-with-resources-and-tags-$(date +%Y-%m).csv.zip"
DBRFILEFS="${2}-aws-billing-detailed-line-items-with-resources-and-tags-$(date +%Y-%m).csv.zip"
DBRFILEFS_CSV="${2}-aws-billing-detailed-line-items-with-resources-and-tags-$(date +%Y-%m).csv"
DBRFILEFS_PARQUET="${2}-aws-billing-detailed-line-items-with-resources-and-tags-$(date +%Y-%m).parquet"

## Fetch current DBR file and unzip
run aws s3 cp $DBRFILES3 /media/ephemeral0/ --quiet
run unzip -qq /media/ephemeral0/$DBRFILEFS -d /media/ephemeral0/


## Check if DBR file contains Blended / Unblended Rates
DBR_BLENDED=`head -1 /media/ephemeral0/$DBRFILEFS_CSV | grep UnBlended | wc -l | egrep -o "\d+"`

run hostname localhost
## Column map requried as Athena only works with lowercase columns.
## Also DBR columns are different depending on Linked Account or without hence alter column map based on that
COLUMN_MAP='"InvoiceID" "invoiceid" "PayerAccountId" "payeraccountid" "LinkedAccountId" "linkedaccountid" "RecordType" "recordtype" "ProductName" "productname" "RateId" "rateid" "SubscriptionId" "subscriptionid" "PricingPlanId" "pricingplanid" "UsageType" "usagetype" "Operation" "operation" "AvailabilityZone" "availabilityzone" "ReservedInstance" "reservedinstance" "ItemDescription" "itemdescription" "UsageStartDate" "usagestartdate" "UsageEndDate" "usageenddate" "UsageQuantity" "usagequantity" '
if [ $DBR_BLENDED -eq 1 ]; then
    COLUMN_MAP+='"BlendedRate" "blendedrate" "BlendedCost" "blendedcost" "UnBlendedRate" "unblendedrate" "UnBlendedCost" "unblendedcost"'
else
    COLUMN_MAP+='"Rate" "rate" "Cost" "cost"'
fi

## Convert CSV to Parquet
run ./csv2parquet /media/ephemeral0/$DBRFILEFS_CSV /media/ephemeral0/$DBRFILEFS_PARQUET --column-map $COLUMN_MAP

## Upload Parquet DBR back to bucket
run aws s3 sync /media/ephemeral0/$DBRFILEFS_PARQUET s3://${1}/dbr-parquet/${2}-$(date +%Y%m) --quiet
