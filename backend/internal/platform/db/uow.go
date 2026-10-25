package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// UnitOfWork ejecuta el flujo de un comando en una transacción y expone la tx
// al resto de colaboradores por el contexto (PP-F1-ARQ-003). Los puertos de
// app/ no conocen pgx: los adaptadores de infra extraen la tx con TxFrom.
type UnitOfWork struct {
	pool *Pool
}

// NewUnitOfWork construye el UoW sobre el pool.
func NewUnitOfWork(pool *Pool) *UnitOfWork { return &UnitOfWork{pool: pool} }

// Do ejecuta fn dentro de una transacción. La tx viaja en el contexto para
// que repositorio, proyectores y outbox compartan la misma transacción;
// confirma si fn devuelve nil y revierte en caso contrario o si hay panic.
func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)
			panic(r)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	txCtx := WithTx(ctx, tx)
	if err = fn(txCtx); err != nil {
		return err
	}
	if err = tx.Commit(txCtx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

type txKey struct{}

// WithTx adjunta una transacción al contexto.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFrom extrae la transacción del contexto; error si no hay (se llamó fuera
// del UoW: error de programación, no de negocio).
func TxFrom(ctx context.Context) (pgx.Tx, error) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok || tx == nil {
		return nil, fmt.Errorf("db: sin transacción en el contexto (¿llamada fuera del UnitOfWork?)")
	}
	return tx, nil
}
