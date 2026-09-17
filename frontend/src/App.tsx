import { useHealth } from '@/api/generated'
import { Button } from '@/components/ui/button'

function App() {
  const { data: health, isPending, isError } = useHealth()

  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4">
      <h1 className="text-2xl font-semibold">Ora Cloud</h1>
      <p className="text-muted-foreground text-sm">
        {isPending && 'checking backend…'}
        {isError && 'backend unreachable'}
        {health && `backend status: ${health.status}`}
      </p>
      <Button>Get started</Button>
    </div>
  )
}

export default App
