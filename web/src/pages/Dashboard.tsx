import { useState } from 'react';
import { useServices } from '@/api/queries';
import { ServiceCard } from '@/components/ServiceCard';
import { ServiceForm } from '@/components/ServiceForm';
import { PlusIcon } from '@/components/icons';

export default function Dashboard() {
  const [addOpen, setAddOpen] = useState(false);
  const services = useServices();

  return (
    <div className="p-6">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <button
          onClick={() => setAddOpen(true)}
          className="flex items-center gap-2 rounded bg-accent px-4 py-2 font-semibold text-white hover:bg-accent-hover"
        >
          <PlusIcon /> Add service
        </button>
      </div>

      {services.isLoading && <div className="text-fg-muted">Loading…</div>}
      {services.isError && <div className="text-danger">Failed to load services.</div>}

      {services.data && services.data.length === 0 && (
        <div className="rounded border border-dashed border-border p-12 text-center text-fg-muted">
          No services yet. Click <span className="text-fg">Add service</span> to get started.
        </div>
      )}

      {services.data && services.data.length > 0 && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {services.data.map((s) => (
            <ServiceCard key={s.id} service={s} />
          ))}
        </div>
      )}

      <ServiceForm open={addOpen} onClose={() => setAddOpen(false)} />
    </div>
  );
}
