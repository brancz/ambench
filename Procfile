a1: alertmanager --storage.path=a1 --web.listen-address=:9093 --cluster.listen-address=:8001 --cluster.peer=127.0.0.1:8002 --cluster.peer=127.0.0.1:8003 --config.file=alertmanager.yaml --cluster.peer-timeout=2s
a2: alertmanager --storage.path=a2 --web.listen-address=:9094 --cluster.listen-address=:8002 --cluster.peer=127.0.0.1:8001 --cluster.peer=127.0.0.1:8003 --config.file=alertmanager.yaml --cluster.peer-timeout=2s
a3: alertmanager --storage.path=a3 --web.listen-address=:9095 --cluster.listen-address=:8003 --cluster.peer=127.0.0.1:8001 --cluster.peer=127.0.0.1:8002 --config.file=alertmanager.yaml --cluster.peer-timeout=2s
p: prometheus --config.file=prometheus.yml
