import { useState } from 'react';
import { Avatar, Button, Modal, ScrollArea } from '@mantine/core';
import { useInfiniteQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api, result, useAuth, type DTO } from './api';
import { Card, State } from './shared';

function Filmography({ person, close }: { person: DTO<'CastDTO'>; close: () => void }) {
  const { t } = useTranslation();
  const userID = useAuth((s) => s.user?.id);
  const films = useInfiniteQuery({
    queryKey: ['native', userID, 'cast-films', person.id],
    initialPageParam: '',
    queryFn: async ({ pageParam, signal }) =>
      result(
        await api.GET('/api/v1/items', {
          params: { query: { personId: person.id, cursor: pageParam || undefined, limit: 36 } },
          signal,
        }),
      ),
    getNextPageParam: (page) => (page.hasMore ? page.nextCursor : undefined),
  });
  const items = films.data?.pages.flatMap((page) => page.items) ?? [];
  return (
    <>
      <p>{t('cast.libraryFilms')}</p>
      <State
        loading={films.isPending}
        error={films.isError && !films.data ? films.error : undefined}
      >
        {items.length === 0 && <p>{t('cast.empty')}</p>}
        <div
          className="poster-grid"
          onClick={(event) => {
            if ((event.target as HTMLElement).closest('a')) close();
          }}
        >
          {items.map((item) => (
            <Card key={item.id} item={item} />
          ))}
        </div>
        {films.isFetchNextPageError && <p role="alert">{t('error')}</p>}
        {films.hasNextPage && (
          <Button
            variant="default"
            loading={films.isFetchingNextPage}
            onClick={() => void films.fetchNextPage()}
          >
            {t(films.isFetchNextPageError ? 'retry' : 'cast.more')}
          </Button>
        )}
      </State>
    </>
  );
}

export default function Cast({ members }: { members: DTO<'CastDTO'>[] }) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<DTO<'CastDTO'>>();
  const token = useAuth((s) => s.token);
  return (
    <section className="film-cast">
      <h2>{t('cast.title')}</h2>
      {members.length === 0 ? (
        <p>{t('cast.unavailable')}</p>
      ) : (
        <ScrollArea type="auto" offsetScrollbars>
          <div className="film-cast-list">
            {members.map((person, index) => (
              <button
                className="film-cast-person"
                aria-label={[person.name, person.character].filter(Boolean).join(' ')}
                key={`${person.id}-${index}`}
                onClick={() => setSelected(person)}
              >
                <Avatar
                  src={
                    person.image ? `${person.image}${encodeURIComponent(token ?? '')}` : undefined
                  }
                  name={person.name}
                  color="initials"
                  size={64}
                  radius="xl"
                  aria-hidden="true"
                />
                <strong title={person.name}>{person.name}</strong>
                {person.character && <span>{person.character}</span>}
              </button>
            ))}
          </div>
        </ScrollArea>
      )}
      <Modal
        opened={!!selected}
        onClose={() => setSelected(undefined)}
        title={selected?.name}
        size="xl"
        scrollAreaComponent={ScrollArea.Autosize}
      >
        {selected && (
          <Filmography key={selected.id} person={selected} close={() => setSelected(undefined)} />
        )}
      </Modal>
    </section>
  );
}
