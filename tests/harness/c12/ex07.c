#include "ft_list.h"
#include <unistd.h>
#include <stdlib.h>

t_list	*ft_list_at(t_list *begin_list, unsigned int nbr);

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

int	main(int argc, char **argv)
{
	t_list			*begin;
	t_list			*elem;
	t_list			*at;
	unsigned int	idx;
	int				i;

	if (argc < 2)
		return (0);
	idx = (unsigned int)atoi(argv[1]);
	begin = NULL;
	i = argc - 1;
	while (i >= 2)
	{
		elem = ft_create_elem(argv[i]);
		elem->next = begin;
		begin = elem;
		i--;
	}
	at = ft_list_at(begin, idx);
	if (at)
		put_str((char *)at->data);
	else
		put_str("(null)");
	write(1, "\n", 1);
	return (0);
}
