package migration

import "fmt"

const managedTablePredicate = `exists (
      select 1
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
      where c.oid = 'public.supawake_heartbeat'::regclass
        and n.nspname = 'public'
        and c.relkind = 'r'
        and obj_description(c.oid, 'pg_class') = 'supawake:heartbeat:v1'
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
            and k.conname = 'supawake_heartbeat_single_row'
            and k.contype = 'c'
            and lower(regexp_replace(
              pg_get_expr(k.conbin, k.conrelid), '[[:space:]()]', '', 'g'
            )) = 'idistrue'
        )
    )`

func InstallSQL() string {
	return fmt.Sprintf(`begin;

do $supawake$
begin
  if to_regclass('public.supawake_heartbeat') is null then
    create table public.supawake_heartbeat (
      id boolean primary key,
      created_at timestamptz not null default now(),
      constraint supawake_heartbeat_single_row check (id is true)
    );

    comment on table public.supawake_heartbeat
      is 'supawake:heartbeat:v1';
  else
    if not %s then
      raise exception
        'public.supawake_heartbeat exists but is not a valid Supawake v1 table';
    end if;
  end if;
end
$supawake$;

insert into public.supawake_heartbeat (id)
values (true)
on conflict (id) do nothing;

alter table public.supawake_heartbeat enable row level security;

revoke all on table public.supawake_heartbeat
from public, anon, authenticated, service_role;

grant select (id)
on table public.supawake_heartbeat
to anon;

drop policy if exists "supawake_read_heartbeat"
on public.supawake_heartbeat;

create policy "supawake_read_heartbeat"
on public.supawake_heartbeat
for select
to anon
using (id is true);

commit;
`, managedTablePredicate)
}

func UninstallSQL() string {
	return fmt.Sprintf(`begin;

do $supawake$
begin
  if to_regclass('public.supawake_heartbeat') is null then
    return;
  end if;

  if not %s then
    raise exception
      'refusing to remove non-Supawake object public.supawake_heartbeat';
  end if;

  drop table public.supawake_heartbeat;
end
$supawake$;

commit;
`, managedTablePredicate)
}
