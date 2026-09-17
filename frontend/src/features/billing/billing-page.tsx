import { format } from 'date-fns'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useBillingSummary, useInvoices } from '@/features/billing/api'

export function BillingPage({ slug }: { slug: string }) {
  const { data: summary, isPending } = useBillingSummary(slug)
  const { data: invoices } = useInvoices(slug)

  return (
    <div className="p-4">
      {isPending && <Skeleton className="h-32 w-full" />}
      {summary && (
        <div className="mb-6 grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Card>
            <CardHeader>
              <CardTitle className="text-xs font-medium text-muted-foreground">Plan</CardTitle>
            </CardHeader>
            <CardContent className="text-xl font-semibold capitalize">{summary.plan}</CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle className="text-xs font-medium text-muted-foreground">Seats</CardTitle>
            </CardHeader>
            <CardContent className="text-xl font-semibold">{summary.seats}</CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle className="text-xs font-medium text-muted-foreground">Renews</CardTitle>
            </CardHeader>
            <CardContent className="text-xl font-semibold">
              {format(new Date(summary.renewalDate), 'MMM d, yyyy')}
            </CardContent>
          </Card>
        </div>
      )}

      <h2 className="mb-2 text-xs font-medium text-muted-foreground">Invoices</h2>
      {invoices && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead>Amount</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {invoices.map((invoice) => (
              <TableRow key={invoice.id}>
                <TableCell>{format(new Date(invoice.date), 'MMM d, yyyy')}</TableCell>
                <TableCell>${invoice.amount}</TableCell>
                <TableCell>
                  <Badge variant={invoice.status === 'paid' ? 'secondary' : 'outline'}>{invoice.status}</Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}
