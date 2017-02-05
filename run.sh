#!/bin/bash


# INSTALL PRE-REQS
curl -s "http://download.nextag.com/apache/drill/drill-1.9.0/apache-drill-1.9.0.tar.gz" | tar xz
yum intall -y java
yum install -y python35

# Start Athena Proxy
PORT=10000 java -cp ./athenaproxy/athenaproxy.jar com.getredash.awsathena_proxy.API .
