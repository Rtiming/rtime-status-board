import { formatTime } from '../../shared/format';
import { dictionary, type Lang } from '../../shared/i18n';
import { StatusDot, statusLabel } from '../../shared/status';
import type { EventView } from '../../types';

export function Events({ events, lang }: { events: EventView[]; lang: Lang }) {
  const t = dictionary[lang];
  return (
    <section className="timeline">
      {events.length === 0 && <div className="empty">{t.noEvents}</div>}
      {events.map((event) => (
        <article className="event" key={event.id}>
          <StatusDot status={event.to} />
          <div>
            <div className="event-title">{event.label}</div>
            <div className="event-meta">
              {event.kind} ·
              {event.from ? `${statusLabel[lang][event.from]} -> ` : ''}
              {statusLabel[lang][event.to]} · {formatTime(event.created_at)}
            </div>
            {event.detail && <p>{event.detail}</p>}
          </div>
        </article>
      ))}
    </section>
  );
}
