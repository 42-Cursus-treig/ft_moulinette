#include "ft_list.h"
#include <unistd.h>

t_list	*ft_list_last(t_list *begin_list);

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
	t_list	*begin;
	t_list	*elem;
	t_list	*last;
	int		i;

	begin = NULL;
	i = 1;
	while (i < argc)
	{
		elem = ft_create_elem(argv[i]);
		elem->next = begin;
		begin = elem;
		i++;
	}
	last = ft_list_last(begin);
	if (last)
	{
		put_str((char *)last->data);
		write(1, "\n", 1);
	}
	return (0);
}
