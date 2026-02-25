import {redirect} from 'next/navigation';

export default function MonitoringAlertsRedirectPage() {
  redirect('/monitoring?tab=alerts');
}
