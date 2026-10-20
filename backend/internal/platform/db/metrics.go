package db

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type collector struct {
	pool                                   *pgxpool.Pool
	total, idle, acquired, constructing    *prometheus.Desc
	maxConns, acquireCount, acquireSeconds *prometheus.Desc
	emptyAcquire, canceledAcquire          *prometheus.Desc
}

func newCollector(pool *pgxpool.Pool) *collector {
	d := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc("pgxpool_"+name, help, nil, nil)
	}
	return &collector{
		pool:            pool,
		total:           d("total_conns", "Total de conexiones en el pool"),
		idle:            d("idle_conns", "Conexiones ociosas"),
		acquired:        d("acquired_conns", "Conexiones en uso"),
		constructing:    d("constructing_conns", "Conexiones en construcción"),
		maxConns:        d("max_conns", "Máximo de conexiones configurado"),
		acquireCount:    d("acquire_count_total", "Adquisiciones acumuladas"),
		acquireSeconds:  d("acquire_duration_seconds_total", "Tiempo acumulado esperando conexión"),
		emptyAcquire:    d("empty_acquire_count_total", "Adquisiciones que esperaron por pool vacío"),
		canceledAcquire: d("canceled_acquire_count_total", "Adquisiciones canceladas por contexto"),
	}
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.total, c.idle, c.acquired, c.constructing, c.maxConns, c.acquireCount, c.acquireSeconds, c.emptyAcquire, c.canceledAcquire} {
		ch <- d
	}
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	g, k := prometheus.GaugeValue, prometheus.CounterValue
	ch <- prometheus.MustNewConstMetric(c.total, g, float64(s.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.idle, g, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.acquired, g, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.constructing, g, float64(s.ConstructingConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConns, g, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.acquireCount, k, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireSeconds, k, s.AcquireDuration().Seconds())
	ch <- prometheus.MustNewConstMetric(c.emptyAcquire, k, float64(s.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.canceledAcquire, k, float64(s.CanceledAcquireCount()))
}
