#include "ft_list.h"
#include <unistd.h>

void	ft_list_foreach(t_list *begin_list, void (*f)(void *));

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static void	print_one(void *data)
{
	put_str((char *)data);
	write(1, "\n", 1);
}

int	main(int argc, char **argv)
{
	t_list	*begin;
	t_list	*elem;
	int		i;

	begin = NULL;
	i = argc - 1;
	while (i >= 1)
	{
		elem = ft_create_elem(argv[i]);
		elem->next = begin;
		begin = elem;
		i--;
	}
	ft_list_foreach(begin, &print_one);
	return (0);
}
