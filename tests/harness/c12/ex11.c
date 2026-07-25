#include "ft_list.h"
#include <unistd.h>

t_list	*ft_list_find(t_list *begin_list, void *data_ref, int (*cmp)());

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static int	ft_strcmp(char *a, char *b)
{
	int	i;

	i = 0;
	while (a[i] && a[i] == b[i])
		i++;
	return ((unsigned char)a[i] - (unsigned char)b[i]);
}

/* argv[1] = data_ref recherchée. Affiche la data trouvée (= argv[1]) ou
   "(null)" si absente. */
int	main(int argc, char **argv)
{
	t_list	*begin;
	t_list	*elem;
	t_list	*found;
	int		i;

	if (argc < 2)
		return (0);
	begin = NULL;
	i = argc - 1;
	while (i >= 2)
	{
		elem = ft_create_elem(argv[i]);
		elem->next = begin;
		begin = elem;
		i--;
	}
	found = ft_list_find(begin, argv[1], &ft_strcmp);
	if (found)
		put_str((char *)found->data);
	else
		put_str("(null)");
	write(1, "\n", 1);
	return (0);
}
