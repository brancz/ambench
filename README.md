# ambench

The tool or collection of tools in this repository is used to perform load and correctness testing against the [Prometheus Alertmanager](https://github.com/prometheus/alertmanager) project.

The Prometheus Alertmanager implements a gossip protocol using eventually consistent delta CRDTs, this was implemented in order for the Alertmanager to have a high availability mode without any additional components. Given that the Alertmanager is a distributed system, the edge cases it yields are sometimes not easily discoverable, this project aims to provide tools to create reproducible functional tests with a predictable outcome.

Ultimately the content of this repository may be useful to be integrated into the Alertmanager test suite, however, at this stage this project is primarily an experiment and testing ground.

## Getting started

Ensure that you have Prometheus in your `$PATH`, as well as the binary of Alertmanager that you are intending to test. In addition to Prometheus and Alertmanager you will need either [goreman](https://github.com/mattn/goreman) or [foreman](https://github.com/ddollar/foreman) in order to start processes according to a `Procfile`.

Build this project.

```bash
make build
```

This will yield the `ambench` binary in the root of this repository. Then start the Alertmanager cluster and a Prometheus instance to monitor it with:

```bash
goreman start
```

Then finally the load tests can be executed with:

```bash
./ambench -alertmanagers=http://localhost:9093,http://localhost:9094,http://localhost:9095
```

## Help and contribute

This project is very young, and any kind of contributions to help and contribute to the stability of the Alertmanager are highly appreciated.

I am by no means a distributed systems expert and the code in this repository is also in a rather rough state, I am open to changing this project in any way that leads to productive testing of the Alertmanger.

## Roadmap

This project so far only implements load production and data rotation over a given dataset with some configurable variables. A non exhaustive list of things that would greatly contribute to the stability of Alertmanager that could be part of this repository could include:

* It would be useful to implement failure injection modes (network, disk, clock, etc.) in order to be able to stress test different scenarios.
* The current report output is entirely a text format. For larger tests it will make sense to index the results and possibly analyze them further.

Other things that might be interesting but possibly orthogonal:

* Possibly this could also act as a suite of conformance tests to verify that an Alertmanager cluster is working as expected.
