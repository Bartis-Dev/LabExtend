import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { connectHC } from '@/api/ws';
import { healthKey } from '@/api/queries';
import { useAuth } from '@/store/auth';
import type { HealthMap } from '@/api/types';

// HealthWatcher opens the /api/ws stream once the user is authenticated
// and pumps each hc_update into the same TanStack Query cache that
// useHealth reads. ServiceCard's status dots therefore re-render without
// any extra plumbing, and the REST polling in useHealth is the fallback
// when the socket is closed.
export function HealthWatcher() {
  const user = useAuth((s) => s.user);
  const qc = useQueryClient();

  useEffect(() => {
    if (!user) return;
    const close = connectHC((msg) => {
      if (msg.type === 'hc_update' && msg.data && typeof msg.data === 'object') {
        qc.setQueryData(healthKey, msg.data as HealthMap);
      }
    });
    return close;
  }, [user, qc]);

  return null;
}
