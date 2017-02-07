#!/bin/bash


# INSTALL PRE-REQS
sudo mkdir /opt/drill
sudo mkdir /opt/drill/log
sudo chmod 777 /opt/drill/log
sudo curl -s "http://download.nextag.com/apache/drill/drill-1.9.0/apache-drill-1.9.0.tar.gz" | sudo tar xz --strip=1 -C /opt/drill
sudo yum install -y java
sudo yum install -y python35
sudo yum install -y aws-cli

# Add Drill to PATH
export PATH=/opt/drill/bin:$PATH

# Start Athena Proxy
PORT=10000 java -cp ./athenaproxy/athenaproxy.jar com.getredash.awsathena_proxy.API .
