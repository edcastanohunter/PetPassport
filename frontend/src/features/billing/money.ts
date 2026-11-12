// Importes de Billing: la API los devuelve en unidades de la moneda (pesos,
// euros), nunca en centavos (PlanPricing, Invoice, BillingPayment…).
export function formatMoney(amount: number, currency = 'COP', locale = 'es'): string {
  const tag = locale === 'en' ? 'en-US' : 'es-CO'
  return new Intl.NumberFormat(tag, {
    style: 'currency',
    currency,
    minimumFractionDigits: 0,
    maximumFractionDigits: currency === 'COP' ? 0 : 2,
  }).format(amount)
}
