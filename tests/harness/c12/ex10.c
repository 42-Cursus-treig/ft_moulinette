#include "ft_list.h"
#include <unistd.h>

void	ft_list_foreach_if(t_list *begin_list, void (*f)(void *), void *data_ref,
			int (*cmp)());

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
	ft_list_foreach_if(begin, &print_one, argv[1], &ft_strcmp);
	return (0);
}
