package main

import (
	"fmt"
	"log/slog"

	clinicalinfra "github.com/edcastanohunter/petpassport/backend/internal/clinical/infra"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityinfra "github.com/edcastanohunter/petpassport/backend/internal/identity/infra"
	orginfra "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/infra"
	petinfra "github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	schedulingapp "github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	schedulinginfra "github.com/edcastanohunter/petpassport/backend/internal/scheduling/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Composición del worker: un solo sitio donde cada módulo aporta tipos de
// evento, upcasters, proyecciones asíncronas, process managers y comandos
// programables. Lo comparten `run` y los modos one-shot (rebuild, resume…).

// newRegistry registra todos los eventos y upcasters y valida la cadena
// (ARQ-005): un binario que no puede leer algún tipo no arranca.
func newRegistry() (*eventstore.Registry, error) {
	reg := eventstore.NewRegistry()
	petinfra.RegisterEvents(reg)
	petinfra.RegisterUpcasters(reg)
	orginfra.RegisterEvents(reg)
	clinicalinfra.RegisterEvents(reg)
	schedulinginfra.RegisterEvents(reg)
	if err := reg.Validate(); err != nil {
		return nil, fmt.Errorf("eventstore registry: %w", err)
	}
	return reg, nil
}

// newSyncRunner construye el runner con las proyecciones primarias de F1
// (síncronas en el api, ADR-009 §1) con las mismas dependencias que el api,
// para `rebuild --sync` (ARQ-006). Las denormalizaciones (nombre del tutor,
// clínica) se resuelven con el estado actual, como en un replay de F1.
func newSyncRunner(pool *db.Pool, reg *eventstore.Registry, logger *slog.Logger) *projection.Runner {
	identitySvc := identityapp.NewService(identityinfra.NewPostgresRepository(pool), logger)
	orgQueries := orginfra.NewQueries(pool.Pool, orginfra.NewUserAdapter(identitySvc))
	runner := projection.NewRunner()
	orginfra.RegisterSyncProjectors(runner, reg)
	petinfra.RegisterSyncProjectors(runner, reg,
		petinfra.NewGuardianAdapter(identitySvc), petinfra.NewOrganizationAdapter(orgQueries))
	clinicalinfra.RegisterSyncProjectors(runner, reg)
	schedulinginfra.RegisterSyncProjectors(runner, reg)
	return runner
}

// registerAsyncProjections registra las proyecciones asíncronas F2 (03 §3)
// en orden de dependencia (ninguna depende de otra en F2). Las primarias de
// F1 siguen síncronas en el api (ADR-009 §1): no se registran aquí.
func registerAsyncProjections(subs *projection.Subscriptions) {
	subs.Register(clinicalinfra.NewTimelineProjector())
	subs.Register(clinicalinfra.NewWeightProjector())
	subs.Register(clinicalinfra.NewCustodyProjector())
	subs.Register(clinicalinfra.NewVaccinationDueProjector())
}

// registerProcessManagers registra los PM del outbox y los comandos que el
// scheduler puede ejecutar. Los PM de F2 (02 §7) llegan con sus agregados:
// complete-on-sign y appointment-timers (M2.3), notifications.rules (M2.4),
// expire-care-link-requests (M2.5)…
func registerProcessManagers(consumer *process.Consumer, allowed *scheduler.AllowedCommands, sched *scheduler.Scheduler) {
	allowed.Allow(func() bus.Command { return schedulingapp.ExpireProposal{} })
	allowed.Allow(func() bus.Command { return schedulingapp.AutoNoShow{} })
	allowed.Allow(func() bus.Command { return schedulingapp.CompleteOnSign{} })
	// Cada PM recibe los Jobs limitados a lo que declara en Emits (Guard).
	consumer.Register(schedulinginfra.NewCompleteOnSign(
		process.Guard(schedulinginfra.NewCompleteOnSign(nil, nil), sched), kernel.RealClock{}))
	consumer.Register(schedulinginfra.NewAppointmentTimers(
		process.Guard(schedulinginfra.NewAppointmentTimers(nil), sched)))
}

// registerCommands registra en el bus del worker los comandos que ejecuta el
// scheduler. Scheduling usa su propio runner de proyecciones síncronas: sus
// comandos solo emiten eventos de Scheduling.
func registerCommands(commands *bus.Bus, pool *db.Pool, store *eventstore.Store, logger *slog.Logger) {
	reg := store.Registry()
	runner := projection.NewRunner()
	schedulinginfra.RegisterSyncProjectors(runner, reg)
	queries := schedulinginfra.NewQueries(pool.Pool)
	schedulingapp.NewService(schedulingapp.Options{
		Appointments: schedulinginfra.NewAppointmentRepository(store, reg, nil, nil),
		Agendas:      schedulinginfra.NewAgendaRepository(store, reg, nil, nil),
		AgendaReader: queries,
		Queries:      queries,
		Reservations: schedulinginfra.NewReservations(pool.Pool),
		UoW:          db.NewUnitOfWork(pool),
		Projects:     schedulinginfra.NewSyncProjector(runner),
		Pets:         schedulinginfra.NewPetAccess(pool.Pool),
		Membership:   schedulinginfra.NewMembership(pool.Pool),
		Logger:       logger,
	}).RegisterCommands(commands)
}
