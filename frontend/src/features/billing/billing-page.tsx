import { format } from 'date-fns'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useBillingSummary, useInvoices } from '@/features/billing/api'

const PLAN_LABELS: Record<string, string> = {
  free: '免费版',
  pro: '专业版',
  business: '企业版',
}

const INVOICE_STATUS_LABELS: Record<string, string> = {
  paid: '已支付',
  pending: '待支付',
}

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
              <CardTitle className="text-xs font-medium text-muted-foreground">套餐</CardTitle>
            </CardHeader>
            <CardContent className="text-xl font-semibold">{PLAN_LABELS[summary.plan] ?? summary.plan}</CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle className="text-xs font-medium text-muted-foreground">席位数</CardTitle>
            </CardHeader>
            <CardContent className="text-xl font-semibold">{summary.seats}</CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle className="text-xs font-medium text-muted-foreground">续订日期</CardTitle>
            </CardHeader>
            <CardContent className="text-xl font-semibold">
              {format(new Date(summary.renewalDate), 'yyyy年M月d日')}
            </CardContent>
          </Card>
        </div>
      )}

      <h2 className="mb-2 text-xs font-medium text-muted-foreground">账单记录</h2>
      {invoices && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>日期</TableHead>
              <TableHead>金额</TableHead>
              <TableHead>状态</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {invoices.map((invoice) => (
              <TableRow key={invoice.id}>
                <TableCell>{format(new Date(invoice.date), 'yyyy年M月d日')}</TableCell>
                <TableCell>¥{invoice.amount}</TableCell>
                <TableCell>
                  <Badge variant={invoice.status === 'paid' ? 'secondary' : 'outline'}>
                    {INVOICE_STATUS_LABELS[invoice.status] ?? invoice.status}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  )
}
