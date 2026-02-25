import {redirect} from 'next/navigation';

export default function MonitoringIntegrationsRedirectPage() {
  redirect('/monitoring?tab=integrations');
}
