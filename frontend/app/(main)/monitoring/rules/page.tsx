import {redirect} from 'next/navigation';

export default function MonitoringRulesRedirectPage() {
  redirect('/monitoring?tab=rules');
}
