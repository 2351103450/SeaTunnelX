import {redirect} from 'next/navigation';

interface MonitoringClusterRedirectPageProps {
  params: Promise<{id: string}>;
}

export default async function MonitoringClusterRedirectPage({
  params,
}: MonitoringClusterRedirectPageProps) {
  const {id} = await params;
  redirect(`/monitoring?tab=dashboard&cluster_id=${id}`);
}
