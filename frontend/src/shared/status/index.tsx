import type { Status } from '../../types';
import type { Lang } from '../i18n';

export const statusLabel: Record<Lang, Record<Status, string>> = {
  en: { ok: 'OK', degraded: 'Degraded', down: 'Down', unknown: 'Unknown', maintenance: 'Maintenance' },
  zh: { ok: '正常', degraded: '降级', down: '故障', unknown: '未知', maintenance: '维护' }
};

export function StatusPill({ status, lang }: { status: Status; lang: Lang }) {
  return (
    <span className={`pill status-${status}`}>
      <StatusDot status={status} />
      {statusLabel[lang][status]}
    </span>
  );
}

export function StatusDot({ status }: { status: Status }) {
  return <span className={`dot status-${status}`} />;
}
