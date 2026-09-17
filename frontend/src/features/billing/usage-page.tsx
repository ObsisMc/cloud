import { format } from 'date-fns'
import { PageHeader } from '@/components/layout/page-header'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { useUsageSeries } from '@/features/billing/api'

function Sparkbars({ values, unit }: { values: number[]; unit: string }) {
  const max = Math.max(...values, 1)
  return (
    <div className="flex h-24 items-end gap-0.5">
      {values.map((v, i) => (
        <div
          key={i}
          className="flex-1 rounded-t-xs bg-primary/70"
          style={{ height: `${Math.max((v / max) * 100, 4)}%` }}
          title={`${v} ${unit}`}
        />
      ))}
    </div>
  )
}

export function UsagePage({ slug }: { slug: string }) {
  const { data: series, isPending } = useUsageSeries(slug)

  const totals = series?.reduce(
    (acc, point) => ({
      agentMinutes: acc.agentMinutes + point.agentMinutes,
      apiCalls: acc.apiCalls + point.apiCalls,
      storageGb: point.storageGb,
    }),
    { agentMinutes: 0, apiCalls: 0, storageGb: 0 },
  )

  return (
    <div className="flex h-full flex-col">
      <PageHeader title="Usage" />
      <div className="flex-1 overflow-y-auto p-4">
        {isPending && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-40 w-full" />
            ))}
          </div>
        )}
        {series && totals && (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Card>
              <CardHeader>
                <CardTitle className="text-xs font-medium text-muted-foreground">
                  Agent minutes (30d) · {totals.agentMinutes.toLocaleString()}
                </CardTitle>
              </CardHeader>
              <CardContent>
                <Sparkbars values={series.map((s) => s.agentMinutes)} unit="min" />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="text-xs font-medium text-muted-foreground">
                  API calls (30d) · {totals.apiCalls.toLocaleString()}
                </CardTitle>
              </CardHeader>
              <CardContent>
                <Sparkbars values={series.map((s) => s.apiCalls)} unit="calls" />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="text-xs font-medium text-muted-foreground">
                  Storage · {totals.storageGb.toFixed(1)} GB
                </CardTitle>
              </CardHeader>
              <CardContent>
                <Sparkbars values={series.map((s) => s.storageGb)} unit="GB" />
              </CardContent>
            </Card>
          </div>
        )}
        {series && (
          <p className="mt-4 text-xs text-muted-foreground">
            {format(new Date(series[0].date), 'MMM d')} – {format(new Date(series[series.length - 1].date), 'MMM d')}
          </p>
        )}
      </div>
    </div>
  )
}
