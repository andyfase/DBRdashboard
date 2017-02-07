#!/bin/bash

chmod 777 /media/ephemeral0
yum install -y git
git clone https://github.com/andyfase/awsDBRanalysis.git
cd awsDBRanalysis
./run.sh
