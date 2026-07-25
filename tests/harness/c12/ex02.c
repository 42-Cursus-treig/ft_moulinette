#include "ft_list.h"
#include <unistd.h>

int	ft_list_size(t_list *begin_list);

static void	put_nbr(int n)
{
	char	buf[12];
	int		i;
	unsigned int	u;

	i = 0;
	if (n < 0)
	{
		write(1, "-", 1);
		u = -(unsigned int)n;
	}
	else
		u = n;
	if (u == 0)
		buf[i++] = '0';
	while (u)
	{
		buf[i++] = '0' + (u % 10);
		u /= 10;
	}
	while (i--)
		write(1, &buf[i], 1);
}

int	main(int argc, char **argv)
{
	t_list	*begin;
	t_list	*elem;
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
	put_nbr(ft_list_size(begin));
	return (0);
}
