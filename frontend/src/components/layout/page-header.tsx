import { ChevronRight } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { SidebarTrigger } from '@/components/ui/sidebar'

export function PageHeader({
  title,
  breadcrumb,
  actions,
}: {
  title: ReactNode
  /** Parent page to link back to, rendered before the title (e.g. Issues > MUL-1). */
  breadcrumb?: { label: string; to: string }
  actions?: ReactNode
}) {
  return (
    <header className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
      <SidebarTrigger className="-ml-1" />
      <div className="flex min-w-0 flex-1 items-center gap-1.5 text-sm font-medium">
        {breadcrumb && (
          <>
            <Link
              to={breadcrumb.to}
              className="shrink-0 text-muted-foreground hover:text-foreground hover:underline"
            >
              {breadcrumb.label}
            </Link>
            <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
          </>
        )}
        <h1 className="min-w-0 truncate">{title}</h1>
      </div>
      {actions}
    </header>
  )
}
