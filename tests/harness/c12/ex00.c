#include "ft_list.h"
#include <unistd.h>
#include <stdlib.h>

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
	t_list	*elem;

	if (argc < 2)
		return (0);
	elem = ft_create_elem(argv[1]);
	if (!elem)
	{
		put_str("NULL\n");
		return (0);
	}
	if (elem->data == (void *)argv[1] && elem->next == NULL)
		put_str("OK\n");
	else
		put_str("KO\n");
	free(elem);
	return (0);
}
