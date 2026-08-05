begin;

do $sb_heartbeat$
begin
  if to_regclass('public.sb_heartbeat') is null then
    create table public.sb_heartbeat (
      id boolean primary key,
      created_at timestamptz not null default now(),
      constraint sb_heartbeat_single_row check (id is true)
    );

    comment on table public.sb_heartbeat
      is 'sb-heartbeat:managed:v1';
  else
    if not exists (
      select 1
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
      where c.oid = 'public.sb_heartbeat'::regclass
        and n.nspname = 'public'
        and c.relkind = 'r'
        and obj_description(c.oid, 'pg_class') = 'sb-heartbeat:managed:v1'
        and (
          select count(*) from pg_attribute a
          where a.attrelid = c.oid and a.attnum > 0 and not a.attisdropped
        ) = 2
        and exists (
          select 1 from pg_attribute a
          where a.attrelid = c.oid and a.attname = 'id'
            and a.atttypid = 'pg_catalog.bool'::regtype and a.attnotnull
        )
        and exists (
          select 1 from pg_attribute a
          where a.attrelid = c.oid and a.attname = 'created_at'
            and a.atttypid = 'pg_catalog.timestamptz'::regtype and a.attnotnull
            and exists (
              select 1 from pg_attrdef d
              where d.adrelid = a.attrelid and d.adnum = a.attnum
            )
        )
        and exists (
          select 1
          from pg_constraint k
          join pg_attribute a
            on a.attrelid = k.conrelid and a.attnum = k.conkey[1]
          where k.conrelid = c.oid and k.contype = 'p'
            and cardinality(k.conkey) = 1 and a.attname = 'id'
        )
        and exists (
          select 1 from pg_constraint k
          where k.conrelid = c.oid
            and k.conname = 'sb_heartbeat_single_row'
            and k.contype = 'c'
            and lower(regexp_replace(
              pg_get_expr(k.conbin, k.conrelid), '[[:space:]()]', '', 'g'
            )) = 'idistrue'
        )
    ) then
      raise exception
        'public.sb_heartbeat exists but is not a valid SB Heartbeat v1 table';
    end if;
  end if;
end
$sb_heartbeat$;

insert into public.sb_heartbeat (id)
values (true)
on conflict (id) do nothing;

alter table public.sb_heartbeat enable row level security;

revoke all on table public.sb_heartbeat
from public, anon, authenticated, service_role;

grant select (id)
on table public.sb_heartbeat
to anon;

drop policy if exists "sb_heartbeat_read"
on public.sb_heartbeat;

create policy "sb_heartbeat_read"
on public.sb_heartbeat
for select
to anon
using (id is true);

commit;
