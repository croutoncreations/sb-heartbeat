begin;

do $sb_heartbeat_release_schema$
declare
  schema_oid oid;
begin
  select n.oid
  into schema_oid
  from pg_namespace n
  where n.nspname = 'sb_heartbeat_release';

  if schema_oid is null then
    create schema sb_heartbeat_release;
    comment on schema sb_heartbeat_release
      is 'sb-heartbeat:release-fixture:v1';
  elsif obj_description(schema_oid, 'pg_namespace')
      is distinct from 'sb-heartbeat:release-fixture:v1' then
    raise exception
      'refusing to use non-SB Heartbeat schema sb_heartbeat_release';
  end if;
end
$sb_heartbeat_release_schema$;

do $sb_heartbeat_release_fixture$
declare
  fixture_is_valid boolean;
begin
  if to_regclass('sb_heartbeat_release.fixture') is null then
    create table sb_heartbeat_release.fixture (
      marker text not null,
      constraint sb_heartbeat_release_fixture_pkey primary key (marker),
      constraint sb_heartbeat_release_fixture_value
        check (marker = 'sb-heartbeat:release-fixture:v1')
    );

    comment on table sb_heartbeat_release.fixture
      is 'sb-heartbeat:release-fixture:v1';
  else
    select
      c.relkind = 'r'
      and obj_description(c.oid, 'pg_class') = 'sb-heartbeat:release-fixture:v1'
      and (
        select count(*) = 1
          and bool_and(a.attname = 'marker' and a.atttypid = 'text'::regtype and a.attnotnull)
        from pg_attribute a
        where a.attrelid = c.oid
          and a.attnum > 0
          and not a.attisdropped
      )
      and (
        select count(*) = 2 and bool_and(
          (k.conname = 'sb_heartbeat_release_fixture_pkey' and k.contype = 'p')
          or (k.conname = 'sb_heartbeat_release_fixture_value' and k.contype = 'c')
        )
        from pg_constraint k
        where k.conrelid = c.oid
      )
    into fixture_is_valid
    from pg_class c
    join pg_namespace n on n.oid = c.relnamespace
    where n.nspname = 'sb_heartbeat_release'
      and c.relname = 'fixture';

    if fixture_is_valid is distinct from true then
      raise exception
        'refusing to replace non-SB Heartbeat object sb_heartbeat_release.fixture';
    end if;

    if exists (
      select 1
      from sb_heartbeat_release.fixture
      where marker <> 'sb-heartbeat:release-fixture:v1'
    ) then
      raise exception
        'refusing to replace non-SB Heartbeat data in sb_heartbeat_release.fixture';
    end if;
  end if;
end
$sb_heartbeat_release_fixture$;

insert into sb_heartbeat_release.fixture (marker)
values ('sb-heartbeat:release-fixture:v1')
on conflict (marker) do nothing;

revoke all on schema sb_heartbeat_release from public, anon, authenticated, service_role;

revoke all on table sb_heartbeat_release.fixture from public, anon, authenticated, service_role;

commit;
