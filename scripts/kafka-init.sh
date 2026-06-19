#!/usr/bin/env bash
set -e
kafka-topics --create \
  --topic heavy-operations \
  --partitions 3 \
  --replication-factor 1 \
  --bootstrap-server kafka:9092 \
  || echo "Topic already exists – continuing"
